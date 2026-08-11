package evidence

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"path/filepath"
	"sort"
	"time"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	ContactSheetSchemaVersion = 1
	ContactSheetTileWidth     = 480
	ContactSheetTileHeight    = 270
	ContactSheetFooterHeight  = 30
	ContactSheetMaxColumns    = 8
	ContactSheetMaxRows       = 8
	ContactSheetMaxCells      = 64
	contactSheetMaxJPEGBytes  = 32 << 20
)

var (
	ErrContactSheetInvalid  = errors.New("evidence contact sheet request is invalid")
	ErrContactSheetTooLarge = errors.New("evidence contact sheet range is too large")
)

type ContactSheetSpec struct {
	From            time.Time
	Columns         uint32
	Rows            uint32
	IntervalSeconds uint32
}

type ContactSheet struct {
	SchemaVersion uint32
	From          time.Time
	To            time.Time
	Columns       uint32
	Rows          uint32
	CellCount     uint32
	ContentType   string
	Filename      string
	Content       []byte
}

// VideoFrameDecoder decodes exact Media Foundation sample offsets from one
// committed Evidence MP4. Every requested offset must be emitted exactly once.
type VideoFrameDecoder interface {
	Decode(context.Context, string, []time.Duration, func(time.Duration, image.Image) error) error
}

type contactCell struct {
	at    time.Time
	state string
	stage string
}

type contactDecodeJob struct {
	segment SegmentManifest
	path    string
	cells   map[time.Duration]int
	seen    map[time.Duration]bool
}

func (s *Store) CreateContactSheet(ctx context.Context, spec ContactSheetSpec, decoder VideoFrameDecoder) (ContactSheet, error) {
	if s == nil || ctx == nil || decoder == nil {
		return ContactSheet{}, fmt.Errorf("%w: store, context, and video decoder are required", ErrContactSheetInvalid)
	}
	spec.From = spec.From.UTC()
	to, err := spec.validate(time.Duration(s.config.MaxRangeSeconds) * time.Second)
	if err != nil {
		return ContactSheet{}, err
	}
	select {
	case s.contact <- struct{}{}:
		defer func() { <-s.contact }()
	case <-ctx.Done():
		return ContactSheet{}, ctx.Err()
	}
	if err = s.requireCommittedRange(spec.From, to); err != nil {
		return ContactSheet{}, err
	}
	segments, err := s.listSegments(ctx, spec.From, to)
	if err != nil {
		return ContactSheet{}, err
	}

	records := make(map[time.Time]struct {
		segment int
		record  Record
	})
	for segmentIndex, segment := range segments {
		for _, record := range segment.Records {
			if !record.ScheduledAt.Before(spec.From) && record.ScheduledAt.Before(to) {
				records[record.ScheduledAt] = struct {
					segment int
					record  Record
				}{segment: segmentIndex, record: record}
			}
		}
	}

	cellCount := int(spec.Columns * spec.Rows)
	cells := make([]contactCell, cellCount)
	jobsBySegment := make(map[int]*contactDecodeJob)
	for index := range cells {
		at := spec.From.Add(time.Duration(index) * time.Duration(spec.IntervalSeconds) * time.Second)
		cells[index] = contactCell{at: at, state: "MISSING"}
		ref, present := records[at]
		if !present {
			continue
		}
		if ref.record.Kind == "gap" {
			cells[index].state = "GAP"
			cells[index].stage = ref.record.Gap.Stage
			continue
		}
		cells[index].state = "FRAME"
		segment := segments[ref.segment]
		if segment.Video == nil {
			return ContactSheet{}, fmt.Errorf("evidence contact sheet frame %s has no committed video", at.Format(time.RFC3339))
		}
		job := jobsBySegment[ref.segment]
		if job == nil {
			job = &contactDecodeJob{
				segment: segment,
				path:    filepath.Join(s.segmentDirectory(segment.Start), segment.Video.File),
				cells:   make(map[time.Duration]int),
				seen:    make(map[time.Duration]bool),
			}
			jobsBySegment[ref.segment] = job
		}
		job.cells[at.Sub(segment.Start)] = index
	}

	width := int(spec.Columns) * ContactSheetTileWidth
	height := int(spec.Rows) * (ContactSheetTileHeight + ContactSheetFooterHeight)
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for index, cell := range cells {
		drawContactPlaceholder(canvas, contactCellRect(index, int(spec.Columns)), index, cell)
	}

	segmentIndexes := make([]int, 0, len(jobsBySegment))
	for index := range jobsBySegment {
		segmentIndexes = append(segmentIndexes, index)
	}
	sort.Ints(segmentIndexes)
	for _, segmentIndex := range segmentIndexes {
		job := jobsBySegment[segmentIndex]
		if err = verifyVideoArtifact(job.path, job.segment.Video); err != nil {
			return ContactSheet{}, err
		}
		offsets := make([]time.Duration, 0, len(job.cells))
		for offset := range job.cells {
			offsets = append(offsets, offset)
		}
		sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
		err = decoder.Decode(ctx, job.path, offsets, func(offset time.Duration, frame image.Image) error {
			cellIndex, requested := job.cells[offset]
			if !requested {
				return fmt.Errorf("evidence decoder emitted unrequested offset %s", offset)
			}
			if job.seen[offset] {
				return fmt.Errorf("evidence decoder emitted duplicate offset %s", offset)
			}
			if frame == nil || frame.Bounds().Dx() != 1920 || frame.Bounds().Dy() != 1080 {
				return fmt.Errorf("evidence decoder emitted invalid frame at offset %s", offset)
			}
			drawContactFrame(canvas, contactCellRect(cellIndex, int(spec.Columns)), cellIndex, cells[cellIndex], frame)
			job.seen[offset] = true
			return nil
		})
		if err != nil {
			return ContactSheet{}, fmt.Errorf("decode committed evidence segment %s: %w", filepath.Base(job.path), err)
		}
		for _, offset := range offsets {
			if !job.seen[offset] {
				return ContactSheet{}, fmt.Errorf("committed evidence frame at offset %s was not decoded from %s", offset, filepath.Base(job.path))
			}
		}
	}

	var output bytes.Buffer
	if err = jpeg.Encode(&output, canvas, &jpeg.Options{Quality: 88}); err != nil {
		return ContactSheet{}, fmt.Errorf("encode evidence contact sheet JPEG: %w", err)
	}
	if output.Len() < 1 || output.Len() > contactSheetMaxJPEGBytes {
		return ContactSheet{}, errors.New("evidence contact sheet JPEG size is invalid")
	}
	return ContactSheet{
		SchemaVersion: ContactSheetSchemaVersion,
		From:          spec.From,
		To:            to,
		Columns:       spec.Columns,
		Rows:          spec.Rows,
		CellCount:     uint32(cellCount),
		ContentType:   "image/jpeg",
		Filename:      fmt.Sprintf("evidence-contact-sheet-%s-%dx%d-%ds.jpg", spec.From.Format("20060102T150405Z"), spec.Columns, spec.Rows, spec.IntervalSeconds),
		Content:       output.Bytes(),
	}, nil
}

func (s ContactSheetSpec) validate(maxRange time.Duration) (time.Time, error) {
	if s.From.IsZero() || !s.From.Equal(s.From.Truncate(time.Second)) {
		return time.Time{}, fmt.Errorf("%w: from must be a whole RFC3339 UTC second", ErrContactSheetInvalid)
	}
	if s.Columns < 1 || s.Columns > ContactSheetMaxColumns || s.Rows < 1 || s.Rows > ContactSheetMaxRows {
		return time.Time{}, fmt.Errorf("%w: columns and rows must each be between 1 and 8", ErrContactSheetInvalid)
	}
	cellCount := uint64(s.Columns) * uint64(s.Rows)
	if cellCount > ContactSheetMaxCells {
		return time.Time{}, fmt.Errorf("%w: grid exceeds %d cells", ErrContactSheetInvalid, ContactSheetMaxCells)
	}
	if s.IntervalSeconds < 1 {
		return time.Time{}, fmt.Errorf("%w: intervalSeconds must be at least 1", ErrContactSheetInvalid)
	}
	spanSeconds := (cellCount-1)*uint64(s.IntervalSeconds) + 1
	if spanSeconds > uint64(maxRange/time.Second) {
		return time.Time{}, fmt.Errorf("%w: requested span %ds exceeds %s", ErrContactSheetTooLarge, spanSeconds, maxRange)
	}
	return s.From.Add(time.Duration(spanSeconds) * time.Second), nil
}

func contactCellRect(index, columns int) image.Rectangle {
	x := index % columns
	y := index / columns
	left := x * ContactSheetTileWidth
	top := y * (ContactSheetTileHeight + ContactSheetFooterHeight)
	return image.Rect(left, top, left+ContactSheetTileWidth, top+ContactSheetTileHeight+ContactSheetFooterHeight)
}

func drawContactFrame(canvas *image.RGBA, bounds image.Rectangle, index int, cell contactCell, frame image.Image) {
	imageBounds := image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Max.X, bounds.Min.Y+ContactSheetTileHeight)
	xdraw.CatmullRom.Scale(canvas, imageBounds, frame, frame.Bounds(), draw.Src, nil)
	drawContactFooter(canvas, bounds, index, cell)
	drawContactBorder(canvas, bounds)
}

func drawContactPlaceholder(canvas *image.RGBA, bounds image.Rectangle, index int, cell contactCell) {
	background := color.RGBA{R: 38, G: 38, B: 42, A: 255}
	if cell.state == "GAP" {
		background = color.RGBA{R: 72, G: 24, B: 24, A: 255}
	}
	imageBounds := image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Max.X, bounds.Min.Y+ContactSheetTileHeight)
	draw.Draw(canvas, imageBounds, image.NewUniform(background), image.Point{}, draw.Src)
	drawScaledLabel(canvas, imageBounds, cell.state, 4)
	drawContactFooter(canvas, bounds, index, cell)
	drawContactBorder(canvas, bounds)
}

func drawContactFooter(canvas *image.RGBA, bounds image.Rectangle, index int, cell contactCell) {
	footer := image.Rect(bounds.Min.X, bounds.Max.Y-ContactSheetFooterHeight, bounds.Max.X, bounds.Max.Y)
	draw.Draw(canvas, footer, image.NewUniform(color.RGBA{A: 255}), image.Point{}, draw.Src)
	label := fmt.Sprintf("#%02d %s %s", index+1, cell.at.Format("2006-01-02 15:04:05Z"), cell.state)
	drawScaledLabel(canvas, footer, label, 2)
}

func drawScaledLabel(canvas *image.RGBA, bounds image.Rectangle, label string, scale int) {
	textWidth := len(label) * 7
	textHeight := 13
	layer := image.NewRGBA(image.Rect(0, 0, textWidth, textHeight+2))
	drawer := font.Drawer{Dst: layer, Src: image.NewUniform(color.White), Face: basicfont.Face7x13, Dot: fixed.P(0, 13)}
	drawer.DrawString(label)
	scaledWidth := textWidth * scale
	scaledHeight := textHeight * scale
	left := bounds.Min.X + (bounds.Dx()-scaledWidth)/2
	top := bounds.Min.Y + (bounds.Dy()-scaledHeight)/2
	target := image.Rect(left, top, left+scaledWidth, top+scaledHeight)
	xdraw.NearestNeighbor.Scale(canvas, target, layer, image.Rect(0, 0, textWidth, textHeight), draw.Over, nil)
}

func drawContactBorder(canvas *image.RGBA, bounds image.Rectangle) {
	border := image.NewUniform(color.RGBA{R: 112, G: 112, B: 112, A: 255})
	draw.Draw(canvas, image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Max.X, bounds.Min.Y+1), border, image.Point{}, draw.Src)
	draw.Draw(canvas, image.Rect(bounds.Min.X, bounds.Max.Y-1, bounds.Max.X, bounds.Max.Y), border, image.Point{}, draw.Src)
	draw.Draw(canvas, image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Min.X+1, bounds.Max.Y), border, image.Point{}, draw.Src)
	draw.Draw(canvas, image.Rect(bounds.Max.X-1, bounds.Min.Y, bounds.Max.X, bounds.Max.Y), border, image.Point{}, draw.Src)
}

func verifyVideoArtifact(path string, artifact *VideoArtifact) error {
	if artifact == nil {
		return errors.New("committed evidence video artifact is required")
	}
	bytes, digest, err := fileIntegrity(path)
	if err != nil {
		return err
	}
	if bytes != artifact.Bytes || digest != artifact.SHA256 {
		return fmt.Errorf("evidence video integrity failed for %s", filepath.Base(path))
	}
	return nil
}

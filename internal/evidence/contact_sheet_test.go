package evidence

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/videocapture"
)

type fakeVideoDecoder struct {
	emitFrames bool
	calls      int
	offsets    []time.Duration
}

func (d *fakeVideoDecoder) Decode(_ context.Context, _ string, offsets []time.Duration, emit func(time.Duration, image.Image) error) error {
	d.calls++
	d.offsets = append(d.offsets, offsets...)
	if !d.emitFrames {
		return nil
	}
	for _, offset := range offsets {
		frame := image.NewNRGBA(image.Rect(0, 0, 1920, 1080))
		for index := 0; index < len(frame.Pix); index += 4 {
			frame.Pix[index] = 20
			frame.Pix[index+1] = 100
			frame.Pix[index+2] = 200
			frame.Pix[index+3] = 255
		}
		if err := emit(offset, frame); err != nil {
			return err
		}
	}
	return nil
}

func TestContactSheetRendersExactFramesGapsAndMissingSlots(t *testing.T) {
	store, err := OpenStore(t.TempDir(), testConfig(), fakeEncoderFactory{})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	if _, err = store.Append(context.Background(), testFrame(at, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Append(context.Background(), videocaptureGap(at.Add(time.Second))); err != nil {
		t.Fatal(err)
	}
	decoder := &fakeVideoDecoder{emitFrames: true}
	sheet, err := store.CreateContactSheet(context.Background(), ContactSheetSpec{
		From: at.Add(-time.Second), Columns: 3, Rows: 1, IntervalSeconds: 1,
	}, decoder)
	if err != nil {
		t.Fatal(err)
	}
	if sheet.SchemaVersion != 1 || sheet.CellCount != 3 || sheet.ContentType != "image/jpeg" || decoder.calls != 1 || len(decoder.offsets) != 1 || decoder.offsets[0] != 0 {
		t.Fatalf("sheet=%+v decoder=%+v", sheet, decoder)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(sheet.Content))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 3*ContactSheetTileWidth || decoded.Bounds().Dy() != ContactSheetTileHeight+ContactSheetFooterHeight {
		t.Fatalf("bounds=%v", decoded.Bounds())
	}
	missing := color.RGBAModel.Convert(decoded.At(ContactSheetTileWidth/2, ContactSheetTileHeight/2)).(color.RGBA)
	frame := color.RGBAModel.Convert(decoded.At(ContactSheetTileWidth+ContactSheetTileWidth/2, ContactSheetTileHeight/2)).(color.RGBA)
	gap := color.RGBAModel.Convert(decoded.At(2*ContactSheetTileWidth+ContactSheetTileWidth/2, ContactSheetTileHeight/2)).(color.RGBA)
	if missing.R < 25 || missing.R > 55 || frame.B < 150 || gap.R < 50 || gap.G > 45 {
		t.Fatalf("missing=%v frame=%v gap=%v", missing, frame, gap)
	}
}

func TestContactSheetFailsWhenCommittedFrameIsNotDecoded(t *testing.T) {
	store, _ := OpenStore(t.TempDir(), testConfig(), fakeEncoderFactory{})
	at := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	_, _ = store.Append(context.Background(), testFrame(at, 1))
	_, _ = store.Append(context.Background(), testFrame(at.Add(time.Second), 2))
	_, err := store.CreateContactSheet(context.Background(), ContactSheetSpec{From: at, Columns: 1, Rows: 1, IntervalSeconds: 1}, &fakeVideoDecoder{})
	if err == nil {
		t.Fatal("missing decoded frame unexpectedly produced a contact sheet")
	}
}

func TestContactSheetFailsBeforeDecodeWhenVideoIntegrityIsInvalid(t *testing.T) {
	store, _ := OpenStore(t.TempDir(), testConfig(), fakeEncoderFactory{})
	at := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	_, _ = store.Append(context.Background(), testFrame(at, 1))
	_, _ = store.Append(context.Background(), testFrame(at.Add(time.Second), 2))
	segments, _ := store.listSegments(context.Background(), at, at.Add(time.Second))
	video := store.segmentDirectory(at) + string(os.PathSeparator) + segments[0].Video.File
	if err := os.WriteFile(video, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	decoder := &fakeVideoDecoder{emitFrames: true}
	_, err := store.CreateContactSheet(context.Background(), ContactSheetSpec{From: at, Columns: 1, Rows: 1, IntervalSeconds: 1}, decoder)
	if err == nil || decoder.calls != 0 {
		t.Fatalf("error=%v decoder calls=%d", err, decoder.calls)
	}
}

func TestContactSheetSpecRejectsInvalidOrOversizedRequests(t *testing.T) {
	at := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name string
		spec ContactSheetSpec
		want error
	}{
		{name: "fractional from", spec: ContactSheetSpec{From: at.Add(time.Nanosecond), Columns: 1, Rows: 1, IntervalSeconds: 1}, want: ErrContactSheetInvalid},
		{name: "zero columns", spec: ContactSheetSpec{From: at, Columns: 0, Rows: 1, IntervalSeconds: 1}, want: ErrContactSheetInvalid},
		{name: "too many columns", spec: ContactSheetSpec{From: at, Columns: 9, Rows: 1, IntervalSeconds: 1}, want: ErrContactSheetInvalid},
		{name: "zero interval", spec: ContactSheetSpec{From: at, Columns: 1, Rows: 1}, want: ErrContactSheetInvalid},
		{name: "range too large", spec: ContactSheetSpec{From: at, Columns: 2, Rows: 1, IntervalSeconds: 60}, want: ErrContactSheetTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.spec.validate(time.Minute)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestContactSheetRejectsActiveUncommittedRange(t *testing.T) {
	store, _ := OpenStore(t.TempDir(), testConfig(), fakeEncoderFactory{})
	at := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	_, _ = store.Append(context.Background(), testFrame(at, 1))
	_, err := store.CreateContactSheet(context.Background(), ContactSheetSpec{From: at, Columns: 1, Rows: 1, IntervalSeconds: 1}, &fakeVideoDecoder{emitFrames: true})
	if !errors.Is(err, ErrRangeNotCommitted) {
		t.Fatalf("error=%v", err)
	}
}

func videocaptureGap(at time.Time) videocapture.Sample {
	return videocapture.Sample{ScheduledAt: at, Stage: "wgc_frame", Err: errors.New("frame unavailable")}
}

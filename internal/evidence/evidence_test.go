package evidence

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/videocapture"
)

func testConfig() Config {
	return Config{SchemaVersion: SchemaVersion, ModuleID: "test/evidence", Kind: "evidence-recorder", Runtime: RuntimeID, TargetExecutable: "Game.exe", Recording: RecordingConfig{Width: 1920, Height: 1080, FramesPerSecond: 1, SegmentSeconds: 2, Codec: "h264", Container: "mp4", Bitrate: 4_000_000, IncludeCursor: false}, FrameTap: FrameTapConfig{Name: `Local\WindowsAgent.Evidence.Test.v1`}, MaxRangeSeconds: 60}
}

type fakeEncoderFactory struct{}

func (fakeEncoderFactory) Open(path string, _ VideoFormat) (SegmentEncoder, error) {
	return &fakeEncoder{path: path}, nil
}

type fakeEncoder struct {
	path   string
	frames []uint64
}

func (e *fakeEncoder) WriteFrame(_ context.Context, index uint64, pixels []byte) error {
	if len(pixels) != 1920*1080*4 {
		return errors.New("unexpected fake frame size")
	}
	e.frames = append(e.frames, index)
	return nil
}

func (e *fakeEncoder) Finalize(context.Context) error {
	return os.WriteFile(e.path, []byte(fmt.Sprintf("fake-mp4:%v", e.frames)), 0o600)
}

func testFrame(at time.Time, sequence uint64) videocapture.Sample {
	frame := &videocapture.Frame{Sequence: sequence, ScheduledAt: at, ObservedAt: at.Add(10 * time.Millisecond), ForegroundExecutable: "Game.exe", Width: 1920, Height: 1080, PixelFormat: videocapture.PixelFormatBGRX32BottomUp, Pixels: make([]byte, 1920*1080*4)}
	return videocapture.Sample{ScheduledAt: at, Frame: frame}
}

func TestConfigRequiresExactVideoRecordingContract(t *testing.T) {
	config := testConfig()
	config.Recording.FramesPerSecond = 2
	if err := config.Validate(); err == nil {
		t.Fatal("expected non-1 FPS config to fail")
	}
	data := []byte(`{"schemaVersion":3,"moduleId":"test/evidence","kind":"evidence-recorder","runtime":"wgc-evidence-video-v1","targetExecutable":"Game.exe","recording":{"width":1920,"height":1080,"framesPerSecond":1,"segmentSeconds":10,"codec":"h264","container":"mp4","bitrate":4000000,"includeCursor":false},"maxRangeSeconds":60,"captureProfile":"1080p-jpeg"}`)
	if _, err := ParseConfig(data); err == nil {
		t.Fatal("legacy screenshot config unexpectedly parsed")
	}
}

func TestStoreCommitsMP4SegmentAndRangeArchive(t *testing.T) {
	store, err := OpenStore(t.TempDir(), testConfig(), fakeEncoderFactory{})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	if _, err = store.Append(context.Background(), testFrame(at, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Append(context.Background(), testFrame(at.Add(time.Second), 2)); err != nil {
		t.Fatal(err)
	}
	if got := store.AvailableThrough(); !got.Equal(at.Add(2 * time.Second)) {
		t.Fatalf("availableThrough=%s", got)
	}
	archive, err := store.CreateArchive(context.Background(), at, at.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(archive.Path)
	reader, err := zip.OpenReader(archive.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var names []string
	var manifest Manifest
	for _, file := range reader.File {
		names = append(names, file.Name)
		if file.Name == "manifest.json" {
			source, _ := file.Open()
			if err = json.NewDecoder(source).Decode(&manifest); err != nil {
				t.Fatal(err)
			}
			source.Close()
		}
	}
	if len(names) != 2 || manifest.FrameCount != 2 || manifest.GapCount != 0 || manifest.MissingCount != 0 || len(manifest.Segments) != 1 || manifest.Segments[0].Video == nil {
		t.Fatalf("unexpected video archive: names=%v manifest=%+v", names, manifest)
	}
}

func TestRangeRejectsActiveUncommittedSegment(t *testing.T) {
	store, _ := OpenStore(t.TempDir(), testConfig(), fakeEncoderFactory{})
	at := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	if _, err := store.Append(context.Background(), testFrame(at, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateArchive(context.Background(), at, at.Add(time.Second)); !errors.Is(err, ErrRangeNotCommitted) {
		t.Fatalf("error=%v", err)
	}
}

func TestGapIsExplicitAndDoesNotCreateSyntheticVideo(t *testing.T) {
	store, _ := OpenStore(t.TempDir(), testConfig(), fakeEncoderFactory{})
	at := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	for offset := 0; offset < 2; offset++ {
		sample := videocapture.Sample{ScheduledAt: at.Add(time.Duration(offset) * time.Second), Stage: "wgc_frame", Err: errors.New("frame unavailable")}
		if _, err := store.Append(context.Background(), sample); err != nil {
			t.Fatal(err)
		}
	}
	segments, err := store.listSegments(context.Background(), at, at.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 || segments[0].FrameCount != 0 || segments[0].GapCount != 2 || segments[0].Video != nil {
		t.Fatalf("segment=%+v", segments)
	}
}

func TestArchiveRejectsCorruptCommittedVideo(t *testing.T) {
	root := t.TempDir()
	store, _ := OpenStore(root, testConfig(), fakeEncoderFactory{})
	at := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	_, _ = store.Append(context.Background(), testFrame(at, 1))
	_, _ = store.Append(context.Background(), testFrame(at.Add(time.Second), 2))
	segments, _ := store.listSegments(context.Background(), at, at.Add(2*time.Second))
	video := filepath.Join(store.segmentDirectory(at), segments[0].Video.File)
	if err := os.WriteFile(video, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateArchive(context.Background(), at, at.Add(2*time.Second)); err == nil {
		t.Fatal("corrupt video unexpectedly exported")
	}
}

type fakeStream struct{ samples []videocapture.Sample }

type fakeTap struct {
	frames int
	err    error
}

func (t *fakeTap) Publish(context.Context, videocapture.Frame) error {
	t.frames++
	return t.err
}

func (s fakeStream) Run(ctx context.Context, interval time.Duration, consume videocapture.Consumer) error {
	if interval != time.Second {
		return errors.New("unexpected interval")
	}
	for _, sample := range s.samples {
		if err := consume(ctx, sample); err != nil {
			return err
		}
	}
	return nil
}

func TestRecorderTapFailureDoesNotStopEvidence(t *testing.T) {
	store, _ := OpenStore(t.TempDir(), testConfig(), fakeEncoderFactory{})
	at := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	tap := &fakeTap{err: errors.New("shared memory unavailable")}
	failures := 0
	recorder := Recorder{Config: testConfig(), Stream: fakeStream{samples: []videocapture.Sample{testFrame(at, 1), testFrame(at.Add(time.Second), 2)}}, Sink: store, FrameTap: tap, OnTapFailed: func(error) { failures++ }}
	if err := recorder.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if failures != 2 || tap.frames != 2 || !store.AvailableThrough().Equal(at.Add(2*time.Second)) {
		t.Fatalf("failures=%d tap=%d available=%s", failures, tap.frames, store.AvailableThrough())
	}
}

func TestRecorderConsumesPCFrameStreamWithoutCaptureRequests(t *testing.T) {
	store, _ := OpenStore(t.TempDir(), testConfig(), fakeEncoderFactory{})
	at := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	tap := &fakeTap{}
	recorder := Recorder{Config: testConfig(), Stream: fakeStream{samples: []videocapture.Sample{testFrame(at, 1)}}, Sink: store, FrameTap: tap}
	if err := recorder.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := store.AvailableThrough(); !got.Equal(at.Add(time.Second)) {
		t.Fatalf("availableThrough=%s", got)
	}
	if tap.frames != 1 {
		t.Fatalf("tap frames=%d", tap.frames)
	}
}

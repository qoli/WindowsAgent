package evidence

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"os"
	"testing"
	"time"
)

func testConfig() Config {
	return Config{SchemaVersion: 1, ModuleID: "test/evidence", Kind: "evidence-recorder", Runtime: RuntimeID, TargetExecutable: "Game.exe", FramesPerSecond: 1, CaptureProfile: "1080p-jpeg", CaptureTimeoutMS: 1000, MaxRangeSeconds: 60}
}
func testJPEG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := jpeg.Encode(&out, image.NewRGBA(image.Rect(0, 0, 1920, 1080)), &jpeg.Options{Quality: 10}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestConfigRequiresExactOneFPS(t *testing.T) {
	config := testConfig()
	config.FramesPerSecond = 2
	if err := config.Validate(); err == nil {
		t.Fatal("expected non-1 FPS config to fail")
	}
	data := []byte(`{"schemaVersion":1,"moduleId":"test/evidence","kind":"evidence-recorder","runtime":"capture-evidence-v1","targetExecutable":"Game.exe","framesPerSecond":1,"captureProfile":"1080p-jpeg","captureTimeoutMs":1000,"maxRangeSeconds":60,"unknown":true}`)
	if _, err := ParseConfig(data); err == nil {
		t.Fatal("expected unknown config field to fail")
	}
}

func TestStoreArchivePreservesFrameAndGap(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	content := testJPEG(t)
	if _, err = store.CommitFrame(context.Background(), at, Frame{CaptureID: "capture-1", ObservedAt: at, ContentType: "image/jpeg", Content: content, Width: 1920, Height: 1080}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CommitGap(context.Background(), at.Add(time.Second), "capture", errors.New("busy")); err != nil {
		t.Fatal(err)
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
	var manifest Manifest
	for _, file := range reader.File {
		if file.Name != "manifest.json" {
			continue
		}
		source, _ := file.Open()
		if err = json.NewDecoder(source).Decode(&manifest); err != nil {
			t.Fatal(err)
		}
		source.Close()
	}
	if manifest.FrameCount != 1 || manifest.GapCount != 1 || manifest.MissingCount != 0 || len(manifest.Records) != 2 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
}

func TestArchiveMakesUnavailableSlotsExplicit(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	at := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	if _, err := store.CommitGap(context.Background(), at.Add(time.Second), "capture", errors.New("busy")); err != nil {
		t.Fatal(err)
	}
	archive, err := store.CreateArchive(context.Background(), at, at.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(archive.Path)
	if archive.Manifest.MissingCount != 2 || len(archive.Manifest.MissingSlots) != 2 || !archive.Manifest.MissingSlots[0].Equal(at) || !archive.Manifest.MissingSlots[1].Equal(at.Add(2*time.Second)) {
		t.Fatalf("missing slots not explicit: %+v", archive.Manifest.MissingSlots)
	}
}

func TestArchiveRejectsCorruptEvidence(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	at := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	content := testJPEG(t)
	record, err := store.CommitFrame(context.Background(), at, Frame{CaptureID: "capture-1", ObservedAt: at, ContentType: "image/jpeg", Content: content, Width: 1920, Height: 1080})
	if err != nil {
		t.Fatal(err)
	}
	dir, _, _ := store.slotPath(at)
	if err = os.WriteFile(dir+string(os.PathSeparator)+record.Frame.File, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateArchive(context.Background(), at, at.Add(time.Second)); err == nil {
		t.Fatal("expected corrupt evidence to fail archive")
	}
}

type failingCapture struct{}

func (failingCapture) Capture(context.Context) (Frame, error) {
	return Frame{}, errors.New("capture busy")
}

type memorySink struct{ frames, gaps int }

func (s *memorySink) CommitFrame(context.Context, time.Time, Frame) (Record, error) {
	s.frames++
	return Record{Kind: "frame"}, nil
}
func (s *memorySink) CommitGap(_ context.Context, at time.Time, _ string, _ error) (Record, error) {
	s.gaps++
	return Record{Kind: "gap", ScheduledAt: at}, nil
}
func TestCaptureFailureCommitsGapWithoutTerminatingSlot(t *testing.T) {
	sink := &memorySink{}
	recorder := Recorder{Config: testConfig(), Capture: failingCapture{}, Sink: sink}
	if err := recorder.recordSlot(context.Background(), time.Now().UTC().Truncate(time.Second)); err != nil {
		t.Fatal(err)
	}
	if sink.gaps != 1 || sink.frames != 0 {
		t.Fatalf("frames=%d gaps=%d", sink.frames, sink.gaps)
	}
}

func TestSlotIsMissedOnlyAfterAFullIntervalOfLateness(t *testing.T) {
	slot := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	if slotMissed(slot, slot.Add(999*time.Millisecond)) {
		t.Fatal("sub-second lateness must not drop a slot")
	}
	if !slotMissed(slot, slot.Add(time.Second)) {
		t.Fatal("one full interval of lateness must drop the slot")
	}
}

package visuallog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventstream"
)

type fakeFrameSource struct {
	frame Frame
	err   error
	calls int
}

type cursorFrameSource struct {
	frames []Frame
	after  []time.Time
}

func (f *cursorFrameSource) Latest(_ context.Context, after time.Time) (Frame, error) {
	f.after = append(f.after, after)
	if len(f.frames) == 0 {
		return Frame{}, ErrNoNewEvidenceFrame
	}
	frame := f.frames[0]
	f.frames = f.frames[1:]
	return frame, nil
}

func (f *fakeFrameSource) Latest(context.Context, time.Time) (Frame, error) {
	f.calls++
	return f.frame, f.err
}

type fakeDescriber struct {
	description Description
	err         error
	calls       int
}

func (f *fakeDescriber) Describe(context.Context, Frame) (Description, error) {
	f.calls++
	return f.description, f.err
}

type fakeAppender struct {
	requests []eventstream.AppendRequest
	err      error
}

func (f *fakeAppender) Append(_ context.Context, request eventstream.AppendRequest) (eventstream.Event, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return eventstream.Event{}, f.err
	}
	return eventstream.Event{Sequence: uint64(len(f.requests)), Type: request.Type}, nil
}

func TestRunnerWarmsWithoutPublishingThenCommitsTimestampedDescription(t *testing.T) {
	config, err := ParseConfig([]byte(validConfigJSON()))
	if err != nil {
		t.Fatal(err)
	}
	frame := testFrame()
	frames := &fakeFrameSource{frame: frame}
	describer := &fakeDescriber{description: Description{
		Text:    "Vast illuminated station interior surrounds large curved industrial structures.",
		ModelID: config.Model.ID, Latency: 1500 * time.Millisecond,
	}}
	events := &fakeAppender{}
	runner := Runner{Config: config, Frames: frames, Describer: describer, Events: events, SessionID: "session_1", InstanceID: "instance_1"}
	var cursor time.Time
	if err := runner.warmup(context.Background(), &cursor); err != nil {
		t.Fatal(err)
	}
	if len(events.requests) != 0 || frames.calls != 1 || describer.calls != 1 {
		t.Fatalf("warmup frames=%d descriptions=%d events=%d", frames.calls, describer.calls, len(events.requests))
	}
	result, err := runner.observe(context.Background(), &cursor)
	if err != nil {
		t.Fatal(err)
	}
	if result.Event == nil || result.Event.Sequence != 1 || result.Dropped != nil || len(events.requests) != 1 {
		t.Fatalf("result = %+v requests=%d", result, len(events.requests))
	}
	request := events.requests[0]
	if request.ObservedAt != frame.ObservedAt || request.Type != config.Output.ObservationType || request.Foreground.Revision != 1 {
		t.Fatalf("unexpected request: %+v", request)
	}
	var payload ObservationPayload
	if err := json.Unmarshal(request.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Timestamp != frame.ObservedAt || payload.Evidence.CaptureID != frame.CaptureID || payload.Evidence.ScheduledAt != frame.ScheduledAt || payload.Description != describer.description.Text || !payload.Untrusted || payload.Model.LatencyMS != 1500 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestRunnerAdvancesEvidenceCursorBetweenWarmupAndObservation(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	first := testFrame()
	second := testFrame()
	second.ScheduledAt = first.ScheduledAt.Add(time.Second)
	second.ObservedAt = first.ObservedAt.Add(time.Second)
	second.CaptureID = "cap_second"
	source := &cursorFrameSource{frames: []Frame{first, second}}
	events := &fakeAppender{}
	runner := Runner{Config: config, Frames: source, Describer: &fakeDescriber{description: Description{Text: "Vast illuminated station interior surrounds large curved industrial docking structures.", ModelID: config.Model.ID}}, Events: events, SessionID: "session_1", InstanceID: "instance_1"}
	var cursor time.Time
	if err := runner.warmup(context.Background(), &cursor); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.observe(context.Background(), &cursor); err != nil {
		t.Fatal(err)
	}
	if len(source.after) != 2 || !source.after[0].IsZero() || source.after[1] != first.ScheduledAt {
		t.Fatalf("cursors=%v", source.after)
	}
	if len(events.requests) != 1 {
		t.Fatalf("events=%d", len(events.requests))
	}
	var payload ObservationPayload
	if err := json.Unmarshal(events.requests[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Evidence.CaptureID != "cap_second" || payload.Evidence.ScheduledAt != second.ScheduledAt {
		t.Fatalf("evidence=%+v", payload.Evidence)
	}
}

func TestRunnerModelFailurePublishesFailureDropsSampleAndContinues(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	cause := errors.New("model output violated description word bound")
	events := &fakeAppender{}
	runner := Runner{
		Config: config, Frames: &fakeFrameSource{frame: testFrame()}, Describer: &fakeDescriber{err: cause},
		Events: events, SessionID: "session_1", InstanceID: "instance_1",
	}
	var cursor time.Time
	result, err := runner.observe(context.Background(), &cursor)
	if err != nil {
		t.Fatal(err)
	}
	if result.Dropped == nil || result.Dropped.Stage != "model" || !errors.Is(result.Dropped.Cause, cause) || result.Event != nil {
		t.Fatalf("result = %+v", result)
	}
	if len(events.requests) != 1 || events.requests[0].Type != config.Output.FailureType {
		t.Fatalf("failure requests = %+v", events.requests)
	}
	var payload FailurePayload
	if err := json.Unmarshal(events.requests[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Stage != "model" || payload.CaptureID != "cap_test" {
		t.Fatalf("failure payload = %+v", payload)
	}
	describer := runner.Describer.(*fakeDescriber)
	describer.err = nil
	describer.description = Description{
		Text:    "Vast illuminated station interior surrounds large curved industrial docking structures.",
		ModelID: config.Model.ID,
	}
	next, err := runner.observe(context.Background(), &cursor)
	if err != nil {
		t.Fatal(err)
	}
	if next.Event == nil || next.Dropped != nil || len(events.requests) != 2 || events.requests[1].Type != config.Output.ObservationType {
		t.Fatalf("next result = %+v requests=%+v", next, events.requests)
	}
}

func TestRunnerNoNewEvidenceWaitsWithoutDropOrEvent(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	events := &fakeAppender{}
	runner := Runner{
		Config: config, Frames: &fakeFrameSource{err: ErrNoNewEvidenceFrame},
		Describer: &fakeDescriber{}, Events: events, SessionID: "session_1", InstanceID: "instance_1",
	}
	var cursor time.Time
	result, err := runner.observe(context.Background(), &cursor)
	if err != nil {
		t.Fatal(err)
	}
	if result.Dropped != nil || result.Event != nil {
		t.Fatalf("result = %+v", result)
	}
	if len(events.requests) != 0 {
		t.Fatalf("capture failure invented events: %+v", events.requests)
	}
}

func TestRunnerJournalFailureDropsSampleAndContinues(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	events := &fakeAppender{err: errors.New("journal temporarily unavailable")}
	runner := Runner{
		Config: config, Frames: &fakeFrameSource{frame: testFrame()}, Describer: &fakeDescriber{description: Description{
			Text: "Vast illuminated station interior surrounds large curved industrial docking structures.", ModelID: config.Model.ID,
		}}, Events: events, SessionID: "session_1", InstanceID: "instance_1",
	}
	var cursor time.Time
	result, err := runner.observe(context.Background(), &cursor)
	if err != nil {
		t.Fatal(err)
	}
	if result.Dropped == nil || result.Dropped.Stage != "journal" || !strings.Contains(result.Dropped.Cause.Error(), "journal temporarily unavailable") {
		t.Fatalf("result = %+v", result)
	}
	events.err = nil
	next, err := runner.observe(context.Background(), &cursor)
	if err != nil {
		t.Fatal(err)
	}
	if next.Event == nil || next.Dropped != nil {
		t.Fatalf("next = %+v", next)
	}
}

func testFrame() Frame {
	return Frame{
		ScheduledAt: time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC), CaptureID: "cap_test", ObservedAt: time.Date(2026, 8, 11, 1, 2, 3, 100000000, time.UTC),
		ContentType: "image/jpeg", Content: []byte("jpeg"), ForegroundRevision: 1, ForegroundExecutable: "EliteDangerous64.exe",
	}
}

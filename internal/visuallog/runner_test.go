package visuallog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/foreground"
)

type fakeCaptureSource struct {
	frame Frame
	err   error
	calls int
}

func (f *fakeCaptureSource) Capture(context.Context) (Frame, error) {
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
	captures := &fakeCaptureSource{frame: frame}
	describer := &fakeDescriber{description: Description{
		Text:    "Vast illuminated station interior surrounds large curved industrial structures.",
		ModelID: config.Model.ID, Latency: 1500 * time.Millisecond,
	}}
	events := &fakeAppender{}
	runner := Runner{Config: config, Capture: captures, Describer: describer, Events: events, SessionID: "session_1", InstanceID: "instance_1"}
	if err := runner.Warmup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(events.requests) != 0 || captures.calls != 1 || describer.calls != 1 {
		t.Fatalf("warmup captures=%d descriptions=%d events=%d", captures.calls, describer.calls, len(events.requests))
	}
	result, err := runner.Observe(context.Background())
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
	if payload.Timestamp != frame.ObservedAt || payload.Description != describer.description.Text || !payload.Untrusted || payload.Model.LatencyMS != 1500 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestRunnerModelFailurePublishesFailureDropsSampleAndContinues(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	cause := errors.New("model output violated description word bound")
	events := &fakeAppender{}
	runner := Runner{
		Config: config, Capture: &fakeCaptureSource{frame: testFrame()}, Describer: &fakeDescriber{err: cause},
		Events: events, SessionID: "session_1", InstanceID: "instance_1",
	}
	result, err := runner.Observe(context.Background())
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
	next, err := runner.Observe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if next.Event == nil || next.Dropped != nil || len(events.requests) != 2 || events.requests[1].Type != config.Output.ObservationType {
		t.Fatalf("next result = %+v requests=%+v", next, events.requests)
	}
}

func TestRunnerCaptureFailureDropsSampleWithoutInventingForegroundFailureEvent(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	events := &fakeAppender{}
	runner := Runner{
		Config: config, Capture: &fakeCaptureSource{err: errors.New("capture unavailable")},
		Describer: &fakeDescriber{}, Events: events, SessionID: "session_1", InstanceID: "instance_1",
	}
	result, err := runner.Observe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Dropped == nil || result.Dropped.Stage != "capture" || !strings.Contains(result.Dropped.Cause.Error(), "capture unavailable") {
		t.Fatalf("result = %+v", result)
	}
	if len(events.requests) != 0 {
		t.Fatalf("capture failure invented events: %+v", events.requests)
	}
}

func testFrame() Frame {
	return Frame{
		CaptureID: "cap_test", ObservedAt: time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC),
		ContentType: "image/jpeg", Content: []byte("jpeg"), ForegroundRevision: 1,
		Foreground: foreground.Info{ExecutableName: "EliteDangerous64.exe"},
	}
}

package visuallog

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type cancellationDescriber struct {
	mu      sync.Mutex
	calls   int
	entered chan struct{}
}

func (d *cancellationDescriber) Describe(ctx context.Context, _ Frame) (Description, error) {
	d.mu.Lock()
	d.calls++
	call := d.calls
	d.mu.Unlock()
	if call == 1 {
		return Description{
			Text:    "Vast illuminated station interior surrounds large curved industrial docking structures.",
			ModelID: "gemma-4-e4b-it-8bit",
		}, nil
	}
	close(d.entered)
	<-ctx.Done()
	return Description{}, ctx.Err()
}

func TestProducerStartsAutomatically(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	runner := Runner{
		Config: config, Frames: &fakeFrameSource{frame: testFrame()},
		Describer: &fakeDescriber{description: Description{
			Text: "Vast illuminated station interior surrounds large curved industrial docking structures.", ModelID: config.Model.ID,
		}},
		Events: &fakeAppender{}, SessionID: "bootstrap_session", InstanceID: "instance_1",
	}
	producer, err := NewProducer(context.Background(), runner)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	started := producer.Status()
	if started.State != StateWarming && started.State != StateActive {
		t.Fatalf("started=%+v", started)
	}
	active := waitForState(t, producer, StateActive)
	if active.SessionID != "bootstrap_session" {
		t.Fatalf("active=%+v", active)
	}
}

func TestProducerCloseIsProcessOwnedCancellationWithoutFailureEvent(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	describer := &cancellationDescriber{entered: make(chan struct{})}
	events := &fakeAppender{}
	runner := Runner{
		Config: config, Frames: &fakeFrameSource{frame: testFrame()},
		Describer: describer, Events: events, SessionID: "bootstrap_session", InstanceID: "instance_1",
	}
	producer, err := NewProducer(context.Background(), runner)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-describer.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("visual log did not enter the first published description")
	}
	producer.Close()
	stopped := waitForState(t, producer, StateStopped)
	if stopped.Error != "" || stopped.DroppedSamples != 0 {
		t.Fatalf("stopped status = %+v", stopped)
	}
	if len(events.requests) != 0 {
		t.Fatalf("cancellation published failure events: %+v", events.requests)
	}
}

type recoveringDescriber struct {
	mu    sync.Mutex
	calls int
}

func (d *recoveringDescriber) Describe(context.Context, Frame) (Description, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if d.calls <= 2 {
		return Description{}, errors.New("model temporarily unavailable")
	}
	return Description{
		Text:    "Vast illuminated station interior surrounds large curved industrial docking structures.",
		ModelID: "gemma-4-e4b-it-8bit",
	}, nil
}

type delayedFrameSource struct {
	mu      sync.Mutex
	enabled bool
	frame   Frame
}

func (s *delayedFrameSource) Latest(context.Context, time.Time) (Frame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled {
		return Frame{}, ErrNoNewEvidenceFrame
	}
	return s.frame, nil
}

func (s *delayedFrameSource) Enable() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = true
}

func TestProducerWaitsForEvidenceThenActivatesWithoutExternalStart(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	config.IntervalMS = 250
	frames := &delayedFrameSource{frame: testFrame()}
	runner := Runner{
		Config: config, Frames: frames, Describer: &fakeDescriber{description: Description{
			Text: "Vast illuminated station interior surrounds large curved industrial docking structures.", ModelID: config.Model.ID,
		}}, Events: &fakeAppender{}, SessionID: "passive_session", InstanceID: "instance_1",
	}
	producer, err := NewProducer(context.Background(), runner)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	time.Sleep(300 * time.Millisecond)
	waiting := producer.Status()
	if waiting.State != StateWarming || waiting.DroppedSamples != 0 {
		t.Fatalf("waiting status = %+v", waiting)
	}
	frames.Enable()
	waitForState(t, producer, StateActive)
}

func TestProducerKeepsAttemptingAfterWarmupAndModelFailures(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	config.IntervalMS = 250
	runner := Runner{
		Config: config, Frames: &fakeFrameSource{frame: testFrame()}, Describer: &recoveringDescriber{},
		Events: &fakeAppender{}, SessionID: "passive_session", InstanceID: "instance_1",
	}
	producer, err := NewProducer(context.Background(), runner)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := producer.Status()
		if status.State == StateActive && status.LastSequence > 0 && status.DroppedSamples >= 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("passive producer did not recover: %+v", producer.Status())
}

func waitForState(t *testing.T, producer *Producer, wanted string) Status {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := producer.Status()
		if status.State == wanted {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("visual log state = %s, want %s", producer.Status().State, wanted)
	return Status{}
}

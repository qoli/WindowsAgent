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

func TestControllerStartsWarmRunRejectsDuplicateAndStopsOnlyVisualLog(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	runner := Runner{
		Config: config, Capture: &fakeCaptureSource{frame: testFrame()},
		Describer: &fakeDescriber{description: Description{
			Text: "Vast illuminated station interior surrounds large curved industrial docking structures.", ModelID: config.Model.ID,
		}},
		Events: &fakeAppender{}, SessionID: "bootstrap_session", InstanceID: "instance_1",
	}
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	controller, err := NewController(root, runner)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	started, err := controller.Start()
	if err != nil || started.State != RunWarming || started.SessionID == "" {
		t.Fatalf("started=%+v error=%v", started, err)
	}
	if _, err := controller.Start(); !errors.Is(err, ErrRunActive) {
		t.Fatalf("duplicate start error = %v", err)
	}
	waitForRunState(t, controller, RunActive)
	stopping, err := controller.Stop()
	if err != nil || stopping.State != RunStopping {
		t.Fatalf("stopping=%+v error=%v", stopping, err)
	}
	waitForRunState(t, controller, RunStopped)
}

func TestControllerStopDuringDescriptionEndsStoppedWithoutFailureEvent(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	describer := &cancellationDescriber{entered: make(chan struct{})}
	events := &fakeAppender{}
	runner := Runner{
		Config: config, Capture: &fakeCaptureSource{frame: testFrame()},
		Describer: describer, Events: events, SessionID: "bootstrap_session", InstanceID: "instance_1",
	}
	controller, err := NewController(context.Background(), runner)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	if _, err := controller.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-describer.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("visual log did not enter the first published description")
	}
	stopping, err := controller.Stop()
	if err != nil || stopping.State != RunStopping {
		t.Fatalf("stopping=%+v error=%v", stopping, err)
	}
	stopped := waitForRunState(t, controller, RunStopped)
	if stopped.Error != "" || stopped.DroppedSamples != 0 {
		t.Fatalf("stopped status = %+v", stopped)
	}
	if len(events.requests) != 0 {
		t.Fatalf("cancellation published failure events: %+v", events.requests)
	}
}

func waitForRunState(t *testing.T, controller *Controller, wanted string) RunStatus {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := controller.Status()
		if status.State == wanted {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("visual log state = %s, want %s", controller.Status().State, wanted)
	return RunStatus{}
}

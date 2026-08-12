package evidence

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/videocapture"
)

type blockingStream struct {
	started chan struct{}
}

func (s blockingStream) Run(ctx context.Context, interval time.Duration, lifecycle videocapture.Lifecycle, _ videocapture.Consumer) error {
	if interval != time.Second {
		return errors.New("unexpected interval")
	}
	if err := lifecycle.Start(); err != nil {
		return err
	}
	defer lifecycle.Stop()
	close(s.started)
	<-ctx.Done()
	return nil
}

func newBlockingController(t *testing.T) (*RunController, *fakeLifecycle) {
	t.Helper()
	store, err := OpenStore(t.TempDir(), testConfig(), fakeEncoderFactory{})
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := &fakeLifecycle{}
	controller, err := NewRunController(context.Background(), Recorder{
		Config: testConfig(), Stream: blockingStream{started: make(chan struct{})},
		Lifecycle: lifecycle, Sink: store, FrameTap: &fakeTap{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return controller, lifecycle
}

func TestRunControllerDefaultsToFiniteTwentyMinuteRun(t *testing.T) {
	controller, lifecycle := newBlockingController(t)
	defer controller.Close()
	controller.newID = func() (string, error) { return "evr_test", nil }

	status, err := controller.Start(RunRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if status.State != RunStarting || status.RunID != "evr_test" || !status.Finite || status.DurationSeconds != 1200 || status.DefaultDurationSeconds != 1200 || status.MaxDurationSeconds != 3600 {
		t.Fatalf("unexpected start status: %+v", status)
	}
	if status.RequestedAt == nil || status.EndsAt == nil || status.EndsAt.Sub(*status.RequestedAt) != 20*time.Minute {
		t.Fatalf("unexpected finite deadline: %+v", status)
	}

	waitForRunState(t, controller, RunRecording, time.Second)
	active, err := controller.Start(RunRequest{})
	if !errors.Is(err, ErrRunActive) || active.RunID != "evr_test" {
		t.Fatalf("duplicate start status=%+v error=%v", active, err)
	}
	if lifecycle.started != 1 {
		t.Fatalf("lifecycle starts=%d", lifecycle.started)
	}
}

func TestRunControllerUsesExplicitDurationAndCompletesAtDeadline(t *testing.T) {
	controller, lifecycle := newBlockingController(t)
	defer controller.Close()
	duration := uint32(1)
	status, err := controller.Start(RunRequest{DurationSeconds: &duration})
	if err != nil {
		t.Fatal(err)
	}
	if status.DurationSeconds != 1 || status.RequestedAt == nil || status.EndsAt == nil || status.EndsAt.Sub(*status.RequestedAt) != time.Second {
		t.Fatalf("unexpected explicit duration: %+v", status)
	}
	completed := waitForRunState(t, controller, RunCompleted, 3*time.Second)
	if completed.CompletedAt == nil || lifecycle.started != 1 || lifecycle.stopped != 1 {
		t.Fatalf("completed=%+v lifecycle=%d/%d", completed, lifecycle.started, lifecycle.stopped)
	}
	retained, err := controller.RunStatus(status.RunID)
	if err != nil || retained.State != RunCompleted {
		t.Fatalf("retained=%+v error=%v", retained, err)
	}
}

func TestRunControllerRejectsDurationsOutsideHardLimit(t *testing.T) {
	controller, _ := newBlockingController(t)
	defer controller.Close()
	for _, duration := range []uint32{0, 3601} {
		if _, err := controller.Start(RunRequest{DurationSeconds: &duration}); !errors.Is(err, ErrDurationInvalid) {
			t.Fatalf("duration=%d error=%v", duration, err)
		}
	}
	if status := controller.Status(); status.State != RunIdle || !status.Finite || status.MaxDurationSeconds != 3600 {
		t.Fatalf("idle status=%+v", status)
	}
}

func TestRunControllerAcceptsOneHourHardLimit(t *testing.T) {
	controller, _ := newBlockingController(t)
	defer controller.Close()
	duration := uint32(3600)
	status, err := controller.Start(RunRequest{DurationSeconds: &duration})
	if err != nil {
		t.Fatal(err)
	}
	if status.DurationSeconds != 3600 || status.RequestedAt == nil || status.EndsAt == nil || status.EndsAt.Sub(*status.RequestedAt) != time.Hour {
		t.Fatalf("unexpected one-hour duration: %+v", status)
	}
}

func waitForRunState(t *testing.T, controller *RunController, state string, timeout time.Duration) RunStatus {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status := controller.Status()
		if status.State == state {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("state never became %q; latest=%+v", state, controller.Status())
	return RunStatus{}
}

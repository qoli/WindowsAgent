package watchdog

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeScheduledTask struct {
	description string
	states      []uint32
	stateIndex  int
	stopErr     error
	runErr      error
	calls       []string
}

func (f *fakeScheduledTask) Description() (string, error) {
	f.calls = append(f.calls, "description")
	return f.description, nil
}

func (f *fakeScheduledTask) State() (uint32, error) {
	f.calls = append(f.calls, "state")
	if len(f.states) == 0 {
		return 0, errors.New("missing fake state")
	}
	state := f.states[f.stateIndex]
	if f.stateIndex < len(f.states)-1 {
		f.stateIndex++
	}
	return state, nil
}

func (f *fakeScheduledTask) Stop() error {
	f.calls = append(f.calls, "stop")
	return f.stopErr
}

func (f *fakeScheduledTask) Run() error {
	f.calls = append(f.calls, "run")
	return f.runErr
}

func TestRestartScheduledTaskStartsStoppedOwnedTask(t *testing.T) {
	task := &fakeScheduledTask{description: "owned", states: []uint32{3}}
	recovery := RecoveryConfig{ScheduledTaskName: "task", ExpectedTaskDescription: "owned"}
	if err := restartScheduledTask(context.Background(), recovery, task); err != nil {
		t.Fatalf("restartScheduledTask: %v", err)
	}
	want := []string{"description", "state", "run"}
	if !reflect.DeepEqual(task.calls, want) {
		t.Fatalf("calls = %v, want %v", task.calls, want)
	}
}

func TestRestartScheduledTaskStopsRunningTaskBeforeStart(t *testing.T) {
	task := &fakeScheduledTask{description: "owned", states: []uint32{taskStateRunning, 3}}
	recovery := RecoveryConfig{ScheduledTaskName: "task", ExpectedTaskDescription: "owned"}
	if err := restartScheduledTask(context.Background(), recovery, task); err != nil {
		t.Fatalf("restartScheduledTask: %v", err)
	}
	want := []string{"description", "state", "stop", "state", "run"}
	if !reflect.DeepEqual(task.calls, want) {
		t.Fatalf("calls = %v, want %v", task.calls, want)
	}
}

func TestRestartScheduledTaskRejectsDescriptionMismatch(t *testing.T) {
	task := &fakeScheduledTask{description: "other", states: []uint32{3}}
	recovery := RecoveryConfig{ScheduledTaskName: "task", ExpectedTaskDescription: "owned"}
	if err := restartScheduledTask(context.Background(), recovery, task); err == nil {
		t.Fatal("restartScheduledTask unexpectedly succeeded")
	}
	want := []string{"description"}
	if !reflect.DeepEqual(task.calls, want) {
		t.Fatalf("calls = %v, want %v", task.calls, want)
	}
}

package watchdog

import (
	"context"
	"fmt"
	"time"
)

const taskStateRunning uint32 = 4

type scheduledTask interface {
	Description() (string, error)
	State() (uint32, error)
	Stop() error
	Run() error
}

func restartScheduledTask(ctx context.Context, recovery RecoveryConfig, task scheduledTask) error {
	description, err := task.Description()
	if err != nil {
		return fmt.Errorf("read scheduled task %q identity: %w", recovery.ScheduledTaskName, err)
	}
	if description != recovery.ExpectedTaskDescription {
		return fmt.Errorf("scheduled task %q description mismatch", recovery.ScheduledTaskName)
	}
	state, err := task.State()
	if err != nil {
		return fmt.Errorf("read scheduled task %q state: %w", recovery.ScheduledTaskName, err)
	}
	if state == taskStateRunning {
		if err := task.Stop(); err != nil {
			return fmt.Errorf("stop scheduled task %q: %w", recovery.ScheduledTaskName, err)
		}
		deadline := time.NewTimer(15 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for state == taskStateRunning {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline.C:
				return fmt.Errorf("scheduled task %q did not stop", recovery.ScheduledTaskName)
			case <-ticker.C:
				state, err = task.State()
				if err != nil {
					return fmt.Errorf("read scheduled task %q state after stop: %w", recovery.ScheduledTaskName, err)
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := task.Run(); err != nil {
		return fmt.Errorf("start scheduled task %q: %w", recovery.ScheduledTaskName, err)
	}
	return nil
}

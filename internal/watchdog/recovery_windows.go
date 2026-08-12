//go:build windows

package watchdog

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/whiteboxsolutions/go-ole"
	"github.com/whiteboxsolutions/go-ole/oleutil"
)

type WindowsTaskRecoverer struct{}

func (WindowsTaskRecoverer) RestartScheduledTask(ctx context.Context, recovery RecoveryConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		return fmt.Errorf("initialize Task Scheduler COM: %w", err)
	}
	defer ole.CoUninitialize()

	unknown, err := oleutil.CreateObject("Schedule.Service")
	if err != nil {
		return fmt.Errorf("create Task Scheduler service: %w", err)
	}
	defer unknown.Release()
	service, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return fmt.Errorf("query Task Scheduler service dispatch: %w", err)
	}
	defer service.Release()
	if err := callTaskSchedulerMethod(service, "Connect"); err != nil {
		return fmt.Errorf("connect to Task Scheduler service: %w", err)
	}

	folderVariant, err := oleutil.CallMethod(service, "GetFolder", `\`)
	if err != nil {
		return fmt.Errorf("open Task Scheduler root folder: %w", err)
	}
	defer folderVariant.Clear()
	folder := folderVariant.ToIDispatch()
	if folder == nil {
		return errors.New("Task Scheduler root folder did not return a dispatch object")
	}
	taskVariant, err := oleutil.CallMethod(folder, "GetTask", recovery.ScheduledTaskName)
	if err != nil {
		return fmt.Errorf("open scheduled task %q: %w", recovery.ScheduledTaskName, err)
	}
	defer taskVariant.Clear()
	task := taskVariant.ToIDispatch()
	if task == nil {
		return fmt.Errorf("scheduled task %q did not return a dispatch object", recovery.ScheduledTaskName)
	}
	target := &comScheduledTask{dispatch: task}
	return restartScheduledTask(ctx, recovery, target)
}

type comScheduledTask struct {
	dispatch *ole.IDispatch
}

func (t *comScheduledTask) Description() (string, error) {
	definitionVariant, err := oleutil.GetProperty(t.dispatch, "Definition")
	if err != nil {
		return "", fmt.Errorf("read task definition: %w", err)
	}
	defer definitionVariant.Clear()
	definition := definitionVariant.ToIDispatch()
	if definition == nil {
		return "", errors.New("task definition did not return a dispatch object")
	}
	registrationVariant, err := oleutil.GetProperty(definition, "RegistrationInfo")
	if err != nil {
		return "", fmt.Errorf("read task registration info: %w", err)
	}
	defer registrationVariant.Clear()
	registration := registrationVariant.ToIDispatch()
	if registration == nil {
		return "", errors.New("task registration info did not return a dispatch object")
	}
	descriptionVariant, err := oleutil.GetProperty(registration, "Description")
	if err != nil {
		return "", fmt.Errorf("read task description: %w", err)
	}
	defer descriptionVariant.Clear()
	return descriptionVariant.ToString(), nil
}

func (t *comScheduledTask) State() (uint32, error) {
	stateVariant, err := oleutil.GetProperty(t.dispatch, "State")
	if err != nil {
		return 0, fmt.Errorf("read task state: %w", err)
	}
	defer stateVariant.Clear()
	return uint32(stateVariant.Val), nil
}

func (t *comScheduledTask) Stop() error {
	return callTaskSchedulerMethod(t.dispatch, "Stop", 0)
}

func (t *comScheduledTask) Run() error {
	return callTaskSchedulerMethod(t.dispatch, "Run", nil)
}

func callTaskSchedulerMethod(dispatch *ole.IDispatch, method string, arguments ...interface{}) error {
	result, err := oleutil.CallMethod(dispatch, method, arguments...)
	if result != nil {
		defer result.Clear()
	}
	return err
}

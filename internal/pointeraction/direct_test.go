package pointeraction

import (
	"context"
	"errors"
	"testing"

	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/windowsinput"
)

func directRequest() DirectClickRequest {
	x, y := 2010, 1100
	return DirectClickRequest{SchemaVersion: 1, Operation: OperationClickScreen, X: &x, Y: &y, HoldMS: 40,
		ExpectedForeground: foreground.Identity{ProcessID: 7, ExecutableName: "Fixture.exe", ExecutablePath: `C:\Apps\Fixture.exe`}}
}

func TestDirectClickUsesNativePixelsWithoutRule(t *testing.T) {
	driver := &recordingPointerDriver{}
	request := directRequest()
	controller, _ := NewController(driver, func() (foreground.Info, error) {
		return foreground.Info{ProcessID: 7, ExecutableName: "Fixture.exe", ExecutablePath: `C:\Apps\Fixture.exe`}, nil
	})
	result, err := controller.ClickDirect(context.Background(), request)
	if err != nil || driver.screenCalls != 1 || driver.screenRequest.X != 2010 || driver.screenRequest.Y != 1100 ||
		result.Foreground != request.ExpectedForeground || result.ScreenWidth != 2560 || result.CoordinateSpace != "native-primary-screen" {
		t.Fatalf("result=%+v request=%+v err=%v", result, driver.screenRequest, err)
	}
	request.Operation, request.X, request.Y = OperationClickCurrentPosition, nil, nil
	if _, err := controller.ClickDirect(context.Background(), request); err != nil || driver.currentCalls != 1 {
		t.Fatalf("current pointer calls=%d err=%v", driver.currentCalls, err)
	}
}

func TestDirectPointerRejectsInvalidRequestAndForegroundDrift(t *testing.T) {
	for _, scenario := range []string{"missing-coordinate", "unexpected-coordinate", "missing-identity", "foreground-mismatch", "foreground-drift"} {
		t.Run(scenario, func(t *testing.T) {
			request := directRequest()
			switch scenario {
			case "missing-coordinate":
				request.X = nil
			case "unexpected-coordinate":
				request.Operation = OperationClickCurrentPosition
			case "missing-identity":
				request.ExpectedForeground.ProcessID = 0
			}
			driver := &recordingPointerDriver{}
			calls := 0
			controller, _ := NewController(driver, func() (foreground.Info, error) {
				calls++
				pid := uint32(7)
				if scenario == "foreground-mismatch" || (scenario == "foreground-drift" && calls == 2) {
					pid++
				}
				return foreground.Info{ProcessID: pid, ExecutableName: "Fixture.exe", ExecutablePath: `C:\Apps\Fixture.exe`}, nil
			})
			_, err := controller.ClickDirect(context.Background(), request)
			if err == nil || driver.screenCalls != 0 || driver.currentCalls != 0 {
				t.Fatalf("err=%v driver=%+v", err, driver)
			}
			if scenario == "foreground-drift" {
				var typed *windowsinput.Error
				if !errors.As(err, &typed) || typed.Code != "INPUT_FOREGROUND_CHANGED" {
					t.Fatalf("error=%v", err)
				}
			}
		})
	}
}

func TestDirectPointerReleaseFailureSurvivesCancellation(t *testing.T) {
	driver := &recordingPointerDriver{err: errors.Join(context.Canceled, &windowsinput.ReleaseError{Cause: errors.New("release failed")})}
	controller, _ := NewController(driver, func() (foreground.Info, error) {
		return foreground.Info{ProcessID: 7, ExecutableName: "Fixture.exe", ExecutablePath: `C:\Apps\Fixture.exe`}, nil
	})
	_, err := controller.ClickDirect(context.Background(), directRequest())
	var typed *windowsinput.Error
	if !errors.As(err, &typed) || typed.Code != "INPUT_RELEASE_FAILED" || !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

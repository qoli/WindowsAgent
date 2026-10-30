package pointeraction

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/windowsinput"
)

const DirectSchemaVersion = 1
const OperationClickScreen = "click"

// DirectClickRequest is a literal operator-selected left click. Coordinates
// are native primary-monitor pixels, never Rule reference coordinates.
type DirectClickRequest struct {
	SchemaVersion      uint32              `json:"schemaVersion"`
	ExpectedForeground foreground.Identity `json:"expectedForeground"`
	Operation          string              `json:"operation"`
	X                  *int                `json:"x,omitempty"`
	Y                  *int                `json:"y,omitempty"`
	HoldMS             uint32              `json:"holdMs"`
}

func (request DirectClickRequest) Validate() error {
	if request.SchemaVersion != DirectSchemaVersion {
		return errors.New("direct pointer schemaVersion must equal 1")
	}
	if err := request.ExpectedForeground.Validate(); err != nil {
		return err
	}
	if request.HoldMS < 1 || request.HoldMS > 2000 {
		return errors.New("direct pointer holdMs must be between 1 and 2000")
	}
	switch request.Operation {
	case OperationClickScreen:
		if request.X == nil || request.Y == nil || *request.X < 0 || *request.Y < 0 {
			return errors.New("click requires nonnegative native primary-screen x and y")
		}
	case OperationClickCurrentPosition:
		if request.X != nil || request.Y != nil {
			return errors.New("click-current-position forbids x and y")
		}
	default:
		return errors.New("direct pointer operation must be click or click-current-position")
	}
	return nil
}

type DirectClickResult struct {
	SchemaVersion   uint32              `json:"schemaVersion"`
	Operation       string              `json:"operation"`
	Foreground      foreground.Identity `json:"foreground"`
	Button          string              `json:"button"`
	CoordinateSpace string              `json:"coordinateSpace"`
	Backend         string              `json:"backend"`
	ScreenX         int                 `json:"screenX"`
	ScreenY         int                 `json:"screenY"`
	ScreenWidth     int                 `json:"screenWidth"`
	ScreenHeight    int                 `json:"screenHeight"`
	HoldMS          int64               `json:"holdMs"`
}

func (c *Controller) ClickDirect(ctx context.Context, request DirectClickRequest) (DirectClickResult, error) {
	if c == nil || ctx == nil {
		return DirectClickResult{}, errors.New("pointer controller and context are required")
	}
	if err := request.Validate(); err != nil {
		return DirectClickResult{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	before, err := c.foreground()
	if err != nil {
		return DirectClickResult{}, &windowsinput.Error{Code: "INPUT_FOREGROUND_UNAVAILABLE", Stage: "resolving-foreground", Cause: err}
	}
	if !request.ExpectedForeground.Matches(before) {
		return DirectClickResult{}, &windowsinput.Error{Code: "INPUT_FOREGROUND_MISMATCH", Stage: "validating-foreground", Cause: errors.New("foreground process does not match expectedForeground")}
	}
	current, err := c.foreground()
	if err != nil {
		return DirectClickResult{}, &windowsinput.Error{Code: "INPUT_FOREGROUND_UNAVAILABLE", Stage: "revalidating-foreground", Cause: err}
	}
	if !request.ExpectedForeground.Matches(current) {
		return DirectClickResult{}, &windowsinput.Error{Code: "INPUT_FOREGROUND_CHANGED", Stage: "revalidating-foreground", Cause: errors.New("foreground process changed before pointer injection")}
	}
	var evidence windowsinput.PointerEvidence
	if request.Operation == OperationClickScreen {
		evidence, err = c.driver.ClickScreen(ctx, windowsinput.ScreenPointerClickRequest{X: *request.X, Y: *request.Y, Hold: time.Duration(request.HoldMS) * time.Millisecond})
	} else {
		evidence, err = c.driver.ClickCurrent(ctx, windowsinput.CurrentPointerClickRequest{Hold: time.Duration(request.HoldMS) * time.Millisecond})
	}
	if err != nil {
		code, stage := "INPUT_INJECTION_FAILED", "injecting-input"
		var releaseError *windowsinput.ReleaseError
		if errors.As(err, &releaseError) {
			code, stage = "INPUT_RELEASE_FAILED", "releasing-input"
		}
		return DirectClickResult{}, &windowsinput.Error{Code: code, Stage: stage, Cause: fmt.Errorf("direct pointer %s: %w", request.Operation, err)}
	}
	return DirectClickResult{SchemaVersion: DirectSchemaVersion, Operation: request.Operation, Foreground: foreground.IdentityOf(current),
		Button: "LEFT", CoordinateSpace: "native-primary-screen", Backend: evidence.Backend, ScreenX: evidence.ScreenX, ScreenY: evidence.ScreenY,
		ScreenWidth: evidence.ScreenWidth, ScreenHeight: evidence.ScreenHeight, HoldMS: evidence.HoldMS}, nil
}

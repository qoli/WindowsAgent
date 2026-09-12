package pointeraction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/windowsinput"
)

type Driver interface {
	ClickReference(context.Context, windowsinput.PointerClickRequest) (windowsinput.PointerEvidence, error)
	ClickCurrent(context.Context, windowsinput.CurrentPointerClickRequest) (windowsinput.PointerEvidence, error)
}

type Controller struct {
	driver     Driver
	foreground func() (foreground.Info, error)
}

func NewController(driver Driver, snapshot func() (foreground.Info, error)) (*Controller, error) {
	if driver == nil || snapshot == nil {
		return nil, errors.New("pointer driver and foreground resolver are required")
	}
	return &Controller{driver: driver, foreground: snapshot}, nil
}

func (c *Controller) Run(ctx context.Context, pkg *Package, inputs map[string]any, ruleID string) (json.RawMessage, error) {
	if err := pkg.ValidateInput(inputs); err != nil {
		return nil, fmt.Errorf("validate pointer Action inputs: %w", err)
	}
	holdMS := 40
	var err error
	if value, ok := inputs["holdMs"]; ok {
		holdMS, err = integer(value)
		if err != nil {
			return nil, err
		}
	}
	before, err := c.foreground()
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(before.ExecutableName, ruleID) {
		return nil, fmt.Errorf("foreground executable is %q, expected owning Rule %q", before.ExecutableName, ruleID)
	}
	current, err := c.foreground()
	if err != nil {
		return nil, err
	}
	if before.ProcessID != current.ProcessID || !strings.EqualFold(current.ExecutableName, ruleID) {
		return nil, errors.New("foreground process changed before pointer injection")
	}
	var evidence windowsinput.PointerEvidence
	result := map[string]any{"schemaVersion": 1, "button": "LEFT"}
	switch pkg.Manifest.Operation {
	case OperationClickReference:
		x, coordinateErr := integer(inputs["x"])
		if coordinateErr != nil {
			return nil, coordinateErr
		}
		y, coordinateErr := integer(inputs["y"])
		if coordinateErr != nil {
			return nil, coordinateErr
		}
		evidence, err = c.driver.ClickReference(ctx, windowsinput.PointerClickRequest{ReferenceX: x, ReferenceY: y, Hold: time.Duration(holdMS) * time.Millisecond})
		result["coordinateSpace"] = "reference-1920x1080-centered"
		result["x"] = evidence.ReferenceX
		result["y"] = evidence.ReferenceY
		result["viewportX"] = evidence.ViewportX
		result["viewportY"] = evidence.ViewportY
		result["viewportWidth"] = evidence.ViewportWidth
		result["viewportHeight"] = evidence.ViewportHeight
	case OperationClickCurrentPosition:
		evidence, err = c.driver.ClickCurrent(ctx, windowsinput.CurrentPointerClickRequest{Hold: time.Duration(holdMS) * time.Millisecond})
		result["coordinateSpace"] = "current-primary-screen-position"
	default:
		return nil, fmt.Errorf("unsupported pointer Action operation %q", pkg.Manifest.Operation)
	}
	if err != nil {
		return nil, err
	}
	result["backend"] = evidence.Backend
	result["screenX"] = evidence.ScreenX
	result["screenY"] = evidence.ScreenY
	result["screenWidth"] = evidence.ScreenWidth
	result["screenHeight"] = evidence.ScreenHeight
	result["holdMs"] = holdMS
	if err := pkg.ValidateOutput(result); err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func integer(v any) (int, error) {
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case json.Number:
		i, e := n.Int64()
		return int(i), e
	default:
		return 0, errors.New("pointer coordinate must be an integer")
	}
}

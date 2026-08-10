package actionlaunch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qoli/WindowsAgent/internal/inputaction"
	"github.com/qoli/WindowsAgent/internal/ocraction"
	"github.com/qoli/WindowsAgent/internal/ocrregionsaction"
	"github.com/qoli/WindowsAgent/internal/rules"
	"github.com/qoli/WindowsAgent/internal/scriptlaunch"
	"github.com/qoli/WindowsAgent/internal/scriptpackage"
	"github.com/qoli/WindowsAgent/internal/streamaction"
	"github.com/qoli/WindowsAgent/internal/strictjson"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Contract is the package-backed model and validation contract for one Action.
type Contract struct {
	Action      rules.Action
	Title       string
	InputSchema json.RawMessage
	input       *jsonschema.Schema
}

func (c Contract) ValidateInput(inputs map[string]any) error {
	if c.input == nil {
		return errors.New("Action input contract is not compiled")
	}
	if inputs == nil {
		return errors.New("Action inputs must be an object")
	}
	if err := c.input.Validate(inputs); err != nil {
		return fmt.Errorf("Action %s input schema: %w", c.Action.ID, err)
	}
	return nil
}

func (e *Executor) Contract(actionID string) (Contract, error) {
	if e == nil {
		return Contract{}, errors.New("Action executor is required")
	}
	action, err := e.rules.ResolveAction(actionID)
	if err != nil {
		return Contract{}, fmt.Errorf("resolve Action %q: %w", actionID, err)
	}
	var title string
	var schemaBytes []byte
	switch action.Runtime {
	case rules.ObservationRuntimeV1:
		pkg, loadErr := scriptpackage.Load(action.Root, action.ID)
		if loadErr != nil {
			return Contract{}, fmt.Errorf("load observation Action %q: %w", action.ID, loadErr)
		}
		title, schemaBytes = pkg.Manifest.Title, pkg.InputSchema
	case rules.PpOcrActionRuntimeV1:
		config, loadErr := ocraction.Load(action.Root)
		if loadErr != nil {
			return Contract{}, fmt.Errorf("load OCR Action %q: %w", action.ID, loadErr)
		}
		title = config.Title
		schemaBytes, err = readContractSchema(action.Root, config.InputSchema)
	case rules.PpOcrTextRegionsActionRuntimeV1:
		config, loadErr := ocrregionsaction.Load(action.Root)
		if loadErr != nil {
			return Contract{}, fmt.Errorf("load OCR text regions Action %q: %w", action.ID, loadErr)
		}
		title = config.Title
		schemaBytes, err = readContractSchema(action.Root, config.InputSchema)
	case rules.WindowsKeyActionRuntimeV1:
		pkg, loadErr := inputaction.Load(action.Root)
		if loadErr != nil {
			return Contract{}, fmt.Errorf("load input Action %q: %w", action.ID, loadErr)
		}
		title, schemaBytes = pkg.Manifest.Title, pkg.InputSchema
	case rules.CompositeActionRuntimeV1, rules.StreamingActionRuntimeV1:
		pkg, loadErr := streamaction.Load(action.Root)
		if loadErr != nil {
			return Contract{}, fmt.Errorf("load Starlark Action %q: %w", action.ID, loadErr)
		}
		title, schemaBytes = pkg.Manifest.Title, pkg.InputSchema
	default:
		return Contract{}, fmt.Errorf("Action %q runtime %q cannot participate in an ephemeral Action Sequence", action.ID, action.Runtime)
	}
	if err != nil {
		return Contract{}, fmt.Errorf("read Action %q input schema: %w", action.ID, err)
	}
	compiled, err := compileContractSchema(action.ID, schemaBytes)
	if err != nil {
		return Contract{}, fmt.Errorf("compile Action %q input schema: %w", action.ID, err)
	}
	return Contract{
		Action: action, Title: title, InputSchema: append(json.RawMessage(nil), schemaBytes...), input: compiled,
	}, nil
}

// ValidateAction resolves and validates one invocation without executing it.
func (e *Executor) ValidateAction(invocation scriptlaunch.Invocation) (rules.Action, error) {
	contract, err := e.Contract(invocation.Capability)
	if err != nil {
		return rules.Action{}, err
	}
	if err := contract.ValidateInput(invocation.Inputs); err != nil {
		return rules.Action{}, err
	}
	return contract.Action, nil
}

func readContractSchema(root, name string) ([]byte, error) {
	info, err := os.Stat(filepath.Join(root, name))
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return nil, errors.New("input schema must be one regular file at most 4 MiB")
	}
	return os.ReadFile(filepath.Join(root, name))
}

func compileContractSchema(actionID string, data []byte) (*jsonschema.Schema, error) {
	if err := strictjson.Validate(data); err != nil {
		return nil, err
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(denyContractSchemaLoader{})
	url := "https://windowsagent.invalid/action-sequence/" + actionID
	if err := compiler.AddResource(url, document); err != nil {
		return nil, err
	}
	return compiler.Compile(url)
}

type denyContractSchemaLoader struct{}

func (denyContractSchemaLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema resource is forbidden: %s", url)
}

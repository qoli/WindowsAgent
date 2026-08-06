package scenereducer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/qoli/WindowsAgent/internal/strictjson"
)

func LoadState(name string, config Config) (State, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return State{}, fmt.Errorf("read reducer state: %w", err)
	}
	if err := strictjson.Validate(data); err != nil {
		return State{}, fmt.Errorf("reducer state must be strict JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state State
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("decode reducer state: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return State{}, errors.New("reducer state contains trailing JSON")
	}
	if err := state.Validate(config); err != nil {
		return State{}, err
	}
	return state, nil
}

func SaveState(name string, state State, config Config) error {
	if err := state.Validate(config); err != nil {
		return err
	}
	return WriteJSONAtomic(name, state)
}

func WriteJSONAtomic(name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode JSON state: %w", err)
	}
	data = append(data, '\n')
	directory := filepath.Dir(name)
	temporary, err := os.CreateTemp(directory, ".scene-reducer-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("restrict temporary state permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary state: %w", err)
	}
	if err := os.Rename(temporaryName, name); err != nil {
		return fmt.Errorf("replace reducer state: %w", err)
	}
	committed = true
	return nil
}

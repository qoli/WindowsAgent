package scenereducer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/strictjson"
)

type Runner struct {
	Config     Config
	Client     *Client
	StatePath  string
	OnProgress func(State) error
}

func (r Runner) Run(ctx context.Context) error {
	if r.Client == nil {
		return errors.New("scene reducer event client is required")
	}
	state, err := LoadState(r.StatePath, r.Config)
	if err != nil {
		return err
	}
	state, err = r.recoverPending(ctx, state)
	if err != nil {
		return err
	}
	if r.OnProgress != nil {
		if err := r.OnProgress(state); err != nil {
			return fmt.Errorf("record reducer progress: %w", err)
		}
	}
	body, err := r.Client.Stream(ctx, state.Cursor)
	if err != nil {
		return err
	}
	defer body.Close()
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), eventstream.MaxEventBytes+1)
	for scanner.Scan() {
		event, err := decodeEvent(scanner.Bytes())
		if err != nil {
			return err
		}
		state, err = r.process(ctx, state, event)
		if err != nil {
			return err
		}
		if r.OnProgress != nil {
			if err := r.OnProgress(state); err != nil {
				return fmt.Errorf("record reducer progress: %w", err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read event stream: %w", err)
	}
	if ctx.Err() != nil {
		return nil
	}
	return errors.New("event stream ended without cancellation")
}

func (r Runner) process(ctx context.Context, state State, event eventstream.Event) (State, error) {
	reduction, err := Reduce(r.Config, state, event)
	if err != nil {
		return State{}, err
	}
	if reduction.Request == nil {
		state = applyCheckpoint(state, reduction.Next)
		if err := SaveState(r.StatePath, state, r.Config); err != nil {
			return State{}, err
		}
		return state, nil
	}
	pending := &PendingOutput{InputSequence: event.Sequence, InputEventID: event.EventID, Request: *reduction.Request, Next: reduction.Next}
	state.Pending = pending
	if err := SaveState(r.StatePath, state, r.Config); err != nil {
		return State{}, err
	}
	appended, err := r.Client.Append(ctx, pending.Request)
	if err != nil {
		return State{}, err
	}
	return r.finalize(state, appended.Sequence)
}

func (r Runner) recoverPending(ctx context.Context, state State) (State, error) {
	if state.Pending == nil {
		return state, nil
	}
	cursor := state.LastOutputSequence
	var found []eventstream.Event
	for {
		replay, err := r.Client.Replay(ctx, cursor, eventstream.MaxReplayLimit)
		if err != nil {
			return State{}, fmt.Errorf("recover pending reducer output: %w", err)
		}
		for _, event := range replay.Events {
			if event.Source.ModuleID == r.Config.ModuleID && event.CausationID == state.Pending.InputEventID && event.Type == state.Pending.Request.Type {
				found = append(found, event)
			}
		}
		cursor = replay.NextCursor
		if cursor >= replay.LastSequence {
			break
		}
	}
	if len(found) > 1 {
		return State{}, errors.New("multiple committed outputs match pending reducer causation")
	}
	if len(found) == 1 {
		return r.finalize(state, found[0].Sequence)
	}
	appended, err := r.Client.Append(ctx, state.Pending.Request)
	if err != nil {
		return State{}, fmt.Errorf("append recovered reducer output: %w", err)
	}
	return r.finalize(state, appended.Sequence)
}

func (r Runner) finalize(state State, outputSequence uint64) (State, error) {
	if state.Pending == nil {
		return State{}, errors.New("cannot finalize reducer state without pending output")
	}
	state = applyCheckpoint(state, state.Pending.Next)
	state.LastOutputSequence = outputSequence
	state.Pending = nil
	if err := SaveState(r.StatePath, state, r.Config); err != nil {
		return State{}, err
	}
	return state, nil
}

func applyCheckpoint(state State, checkpoint Checkpoint) State {
	state.Cursor = checkpoint.Cursor
	state.LastSummarySequence = checkpoint.LastSummarySequence
	state.LastSummaryObservedAt = checkpoint.LastSummaryObservedAt
	state.Scene = checkpoint.Scene
	state.Baseline = checkpoint.Baseline
	return state
}

func decodeEvent(data []byte) (eventstream.Event, error) {
	if err := strictjson.Validate(data); err != nil {
		return eventstream.Event{}, fmt.Errorf("stream event must be strict JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var event eventstream.Event
	if err := decoder.Decode(&event); err != nil {
		return eventstream.Event{}, fmt.Errorf("decode stream event: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return eventstream.Event{}, errors.New("stream event contains trailing JSON")
	}
	if event.Sequence == 0 || event.EventID == "" {
		return eventstream.Event{}, errors.New("stream event identity is invalid")
	}
	return event, nil
}

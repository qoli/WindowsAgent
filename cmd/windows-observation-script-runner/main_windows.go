//go:build windows && amd64

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/qoli/WindowsAgent/internal/observationapi"
	"github.com/qoli/WindowsAgent/internal/observationprotocol"
	"github.com/qoli/WindowsAgent/internal/scriptpackage"
	"github.com/qoli/WindowsAgent/internal/scriptrunner"
)

type initializeParams struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	JobID           string                 `json:"jobId"`
	Deadline        time.Time              `json:"deadline"`
	Package         scriptpackage.Identity `json:"package"`
}

type runParams struct {
	JobID  string         `json:"jobId"`
	Inputs map[string]any `json:"inputs"`
}

type broker struct {
	conn  *observationprotocol.Conn
	jobID string
	mu    sync.Mutex
	next  uint64
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	flags := flag.NewFlagSet("windows-observation-script-runner", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var packageRoot string
	flags.StringVar(&packageRoot, "package-root", "", "host-resolved absolute observation package root")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || packageRoot == "" || !filepath.IsAbs(packageRoot) {
		return errors.New("--package-root must be the only argument and must be absolute")
	}
	pkg, err := scriptpackage.Load(packageRoot)
	if err != nil {
		return fmt.Errorf("load observation package: %w", err)
	}
	conn, err := observationprotocol.NewConn(
		os.Stdin,
		os.Stdout,
		observationprotocol.DefaultMaxFrameBytes,
	)
	if err != nil {
		return err
	}
	first, err := conn.Read()
	if err != nil {
		return err
	}
	if first.Method != "initialize" {
		return errors.New("first script-runner request must be initialize")
	}
	var initialize initializeParams
	if err := decodeParams(first.Params, &initialize); err != nil {
		writeError(conn, first.ID, -32602, "invalid initialize params", err)
		return err
	}
	if initialize.ProtocolVersion != observationapi.ProtocolVersion ||
		initialize.JobID == "" || !initialize.Deadline.After(time.Now()) ||
		initialize.Package != pkg.Identity {
		err := errors.New("protocol, job, deadline, or package identity does not match")
		writeError(conn, first.ID, -32602, "invalid initialize params", err)
		return err
	}
	initialized, _ := json.Marshal(map[string]any{
		"protocolVersion": observationapi.ProtocolVersion,
		"jobId":           initialize.JobID,
		"package":         pkg.Identity,
	})
	if err := conn.Write(observationprotocol.Message{ID: first.ID, Result: initialized}); err != nil {
		return err
	}
	message, err := conn.Read()
	if err != nil {
		return err
	}
	if message.Method != "script/run" {
		return errors.New("runner accepts exactly one script/run after initialize")
	}
	var params runParams
	if err := decodeParams(message.Params, &params); err != nil {
		writeError(conn, message.ID, -32602, "invalid script/run params", err)
		return err
	}
	if params.JobID != initialize.JobID {
		err := errors.New("script/run jobId does not match initialized session")
		writeError(conn, message.ID, -32602, "invalid script/run params", err)
		return err
	}
	scriptRunner, err := scriptrunner.New(&broker{conn: conn, jobID: initialize.JobID})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithDeadline(context.Background(), initialize.Deadline)
	defer cancel()
	output, err := scriptRunner.Run(ctx, pkg, params.Inputs)
	if err != nil {
		var typed *scriptrunner.Error
		data := map[string]any{"code": "SCRIPT_RUNTIME_FAILED", "stage": "executing-script"}
		messageText := err.Error()
		if errors.As(err, &typed) {
			data["code"] = typed.Code
			data["stage"] = typed.Stage
			if typed.Cause != nil {
				messageText = typed.Cause.Error()
			}
		}
		encoded, _ := json.Marshal(data)
		return conn.Write(observationprotocol.Message{
			ID: message.ID,
			Error: &observationprotocol.ResponseError{
				Code:    -32020,
				Message: messageText,
				Data:    encoded,
			},
		})
	}
	if err := conn.Write(observationprotocol.Message{ID: message.ID, Result: output}); err != nil {
		return err
	}
	shutdown, err := conn.Read()
	if err != nil {
		return err
	}
	if shutdown.Method != "shutdown" {
		return errors.New("script/run must be followed by shutdown")
	}
	result, _ := json.Marshal(map[string]any{"jobId": initialize.JobID})
	return conn.Write(observationprotocol.Message{ID: shutdown.ID, Result: result})
}

func (b *broker) Call(_ context.Context, namespace, operation string, arguments map[string]any) (any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	id := "broker-" + strconv.FormatUint(b.next, 10)
	encodedArguments, err := json.Marshal(arguments)
	if err != nil {
		return nil, err
	}
	call := observationapi.Call{
		JobID:          b.jobID,
		ObserverCallID: id,
		Namespace:      namespace,
		Operation:      operation,
		Arguments:      encodedArguments,
	}
	params, err := json.Marshal(call)
	if err != nil {
		return nil, err
	}
	if err := b.conn.Write(observationprotocol.Message{
		ID:     id,
		Method: "broker/call",
		Params: params,
	}); err != nil {
		return nil, err
	}
	response, err := b.conn.Read()
	if err != nil {
		return nil, err
	}
	if response.ID != id || response.Method != "" {
		return nil, errors.New("broker response does not match pending call")
	}
	if response.Error != nil {
		var observerError observationapi.Error
		if err := decodeParams(response.Error.Data, &observerError); err != nil {
			return nil, &scriptrunner.BrokerError{
				Code:  "OBSERVER_PROTOCOL_INVALID",
				Cause: fmt.Errorf("decode broker error data: %w", err),
			}
		}
		if observerError.Kind == "" {
			return nil, &scriptrunner.BrokerError{
				Code:  "OBSERVER_PROTOCOL_INVALID",
				Cause: errors.New("broker error kind is empty"),
			}
		}
		return nil, &scriptrunner.BrokerError{
			Code:             observerError.Kind,
			FallbackEligible: observerError.Kind == "OBSERVER_CALL_FAILED",
			Cause:            errors.New(response.Error.Message),
		}
	}
	var result observationapi.Result
	if err := decodeParams(response.Result, &result); err != nil {
		return nil, fmt.Errorf("decode broker result: %w", err)
	}
	if result.JobID != b.jobID || result.ObserverCallID != id ||
		result.Namespace != namespace || result.Operation != operation {
		return nil, errors.New("broker result envelope does not match request")
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(result.Value))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func decodeParams(data json.RawMessage, target any) error {
	if len(data) == 0 {
		return errors.New("params are required")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	return decoder.Decode(target)
}

func writeError(conn *observationprotocol.Conn, id string, code int, message string, cause error) error {
	data, _ := json.Marshal(map[string]any{"cause": cause.Error()})
	return conn.Write(observationprotocol.Message{
		ID: id,
		Error: &observationprotocol.ResponseError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	})
}

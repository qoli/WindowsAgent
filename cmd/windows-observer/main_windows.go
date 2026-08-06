//go:build windows && amd64

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/qoli/WindowsAgent/internal/observationapi"
	"github.com/qoli/WindowsAgent/internal/observationprotocol"
	"github.com/qoli/WindowsAgent/internal/observer"
	"github.com/qoli/WindowsAgent/internal/scriptpackage"
	"github.com/qoli/WindowsAgent/internal/wgc"
)

type initializeParams struct {
	ProtocolVersion   string                    `json:"protocolVersion"`
	JobID             string                    `json:"jobId"`
	Deadline          time.Time                 `json:"deadline"`
	Permissions       scriptpackage.Permissions `json:"permissions"`
	Process           *observer.ProcessIdentity `json:"process"`
	ResolvedFileRoots map[string]string         `json:"resolvedFileRoots"`
	BlobRoot          string                    `json:"blobRoot"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
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
		return errors.New("first observer request must be initialize")
	}
	var params initializeParams
	if err := decodeParams(first.Params, &params); err != nil {
		writeError(conn, first.ID, -32602, "invalid initialize params", err)
		return err
	}
	if params.ProtocolVersion != observationapi.ProtocolVersion ||
		params.JobID == "" || params.Deadline.IsZero() {
		err := errors.New("protocolVersion, jobId, and deadline must match the host session")
		writeError(conn, first.ID, -32602, "invalid initialize params", err)
		return err
	}
	if !params.Deadline.After(time.Now()) {
		err := errors.New("observer deadline is already expired")
		writeError(conn, first.ID, -32602, "invalid initialize params", err)
		return err
	}
	router := observer.RouterBackend{}
	if params.Permissions.Memory != nil {
		if params.Process == nil {
			return errors.New("memory permission requires an exact process identity")
		}
		router.Memory, err = observer.NewMemoryBackend(*params.Process, params.Permissions.Memory.MaxBytesRead)
		if err != nil {
			writeError(conn, first.ID, -32010, "initialize memory observer", err)
			return err
		}
	}
	if params.Permissions.File != nil {
		router.File, err = observer.NewFileBackendWithBlobRoot(params.ResolvedFileRoots, params.BlobRoot)
		if err != nil {
			router.Close()
			writeError(conn, first.ID, -32011, "initialize file observer", err)
			return err
		}
	}
	if params.Permissions.Screen != nil {
		if params.Process == nil {
			return errors.New("screen permission requires an exact process identity")
		}
		capturer, captureErr := wgc.New(slog.New(slog.NewJSONHandler(io.Discard, nil)))
		if captureErr != nil {
			router.Close()
			writeError(conn, first.ID, -32013, "initialize screen observer", captureErr)
			return captureErr
		}
		router.Screen, err = observer.NewScreenBackend(capturer, *params.Process, params.Permissions.Screen.MaxPixels)
		if err != nil {
			router.Close()
			writeError(conn, first.ID, -32013, "initialize screen observer", err)
			return err
		}
	}
	defer router.Close()
	session, err := observer.NewSession(params.JobID, params.Permissions, router)
	if err != nil {
		writeError(conn, first.ID, -32012, "initialize observer session", err)
		return err
	}
	initialized, _ := json.Marshal(map[string]any{
		"protocolVersion": observationapi.ProtocolVersion,
		"jobId":           params.JobID,
	})
	if err := conn.Write(observationprotocol.Message{ID: first.ID, Result: initialized}); err != nil {
		return err
	}
	ctx, cancel := context.WithDeadline(context.Background(), params.Deadline)
	defer cancel()
	for {
		message, err := conn.Read()
		if err != nil {
			return err
		}
		switch message.Method {
		case "observer/call":
			var call observationapi.Call
			if err := decodeParams(message.Params, &call); err != nil {
				if writeErr := writeError(conn, message.ID, -32602, "invalid observer call", err); writeErr != nil {
					return writeErr
				}
				continue
			}
			result, err := session.Call(ctx, call)
			if err != nil {
				if writeErr := writeObserverError(conn, message.ID, err); writeErr != nil {
					return writeErr
				}
				continue
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				return err
			}
			if err := conn.Write(observationprotocol.Message{ID: message.ID, Result: encoded}); err != nil {
				return err
			}
		case "shutdown":
			result, _ := json.Marshal(map[string]any{"jobId": params.JobID})
			return conn.Write(observationprotocol.Message{ID: message.ID, Result: result})
		default:
			if err := writeError(conn, message.ID, -32601, "method not found", errors.New(message.Method)); err != nil {
				return err
			}
		}
	}
}

func decodeParams(data json.RawMessage, target any) error {
	if len(data) == 0 {
		return errors.New("params are required")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeObserverError(conn *observationprotocol.Conn, id string, err error) error {
	var typed *observationapi.Error
	if !errors.As(err, &typed) {
		return writeError(conn, id, -32030, "observer call failed", err)
	}
	data, _ := json.Marshal(typed)
	return conn.Write(observationprotocol.Message{
		ID: id,
		Error: &observationprotocol.ResponseError{
			Code:    -32030,
			Message: typed.Kind,
			Data:    data,
		},
	})
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

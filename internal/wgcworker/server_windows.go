//go:build windows && amd64

package wgcworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/observationprotocol"
	"github.com/qoli/WindowsAgent/internal/wgc"
)

func Serve(reader io.Reader, writer io.Writer, logger *slog.Logger) error {
	if logger == nil {
		return errors.New("WGC worker logger is required")
	}
	conn, err := observationprotocol.NewConn(reader, writer, MaxFrameBytes)
	if err != nil {
		return err
	}
	first, err := conn.Read()
	if err != nil {
		return err
	}
	if first.Method != methodInitialize {
		return errors.New("first WGC worker request must be initialize")
	}
	var params initializeParams
	if err := decodeStrict(first.Params, &params); err != nil {
		_ = writeRPCError(conn, first.ID, -32602, "invalid initialize params", err)
		return err
	}
	if params.ProtocolVersion != ProtocolVersion {
		err := fmt.Errorf("protocolVersion must equal %s", ProtocolVersion)
		_ = writeRPCError(conn, first.ID, -32602, "invalid initialize params", err)
		return err
	}
	capturer, err := wgc.NewPersistent(logger)
	if err != nil {
		_ = writeCaptureError(conn, first.ID, "initialize persistent WGC", err)
		return err
	}
	defer capturer.Close()
	capturer.SetTrace(params.Trace)
	status, err := capturer.Status(context.Background())
	if err != nil {
		_ = writeCaptureError(conn, first.ID, "read persistent WGC status", err)
		return err
	}
	if err := writeResult(conn, first.ID, initializeResult{
		ProtocolVersion: ProtocolVersion,
		ProcessID:       os.Getpid(),
		Backend:         "windows-graphics-capture",
		Persistent:      true,
		Status:          status,
	}); err != nil {
		return err
	}

	for {
		message, err := conn.Read()
		if err != nil {
			return err
		}
		switch message.Method {
		case methodStatus:
			var request deadlineParams
			if err := decodeStrict(message.Params, &request); err != nil {
				if writeErr := writeRPCError(conn, message.ID, -32602, "invalid status params", err); writeErr != nil {
					return writeErr
				}
				continue
			}
			_, cancel, err := callContext(request.Deadline)
			if err != nil {
				if writeErr := writeRPCError(conn, message.ID, -32602, "invalid status deadline", err); writeErr != nil {
					return writeErr
				}
				continue
			}
			cancel()
			status, err := capturer.Status(context.Background())
			if err != nil {
				if writeErr := writeCaptureError(conn, message.ID, "read persistent WGC status", err); writeErr != nil {
					return writeErr
				}
				continue
			}
			if err := writeResult(conn, message.ID, status); err != nil {
				return err
			}
		case methodCapture:
			var request captureParams
			if err := decodeStrict(message.Params, &request); err != nil {
				if writeErr := writeRPCError(conn, message.ID, -32602, "invalid full capture params", err); writeErr != nil {
					return writeErr
				}
				continue
			}
			ctx, cancel, err := callContext(request.Deadline)
			if err != nil {
				if writeErr := writeRPCError(conn, message.ID, -32602, "invalid full capture deadline", err); writeErr != nil {
					return writeErr
				}
				continue
			}
			result, callErr := capturer.Capture(ctx, request.Request)
			cancel()
			if callErr != nil {
				if writeErr := writeCaptureError(conn, message.ID, "persistent full capture failed", callErr); writeErr != nil {
					return writeErr
				}
				continue
			}
			if err := writeResult(conn, message.ID, captureResult{Result: result}); err != nil {
				return err
			}
		case methodCaptureRegion:
			var request regionParams
			if err := decodeStrict(message.Params, &request); err != nil {
				if writeErr := writeRPCError(conn, message.ID, -32602, "invalid region capture params", err); writeErr != nil {
					return writeErr
				}
				continue
			}
			ctx, cancel, err := callContext(request.Deadline)
			if err != nil {
				if writeErr := writeRPCError(conn, message.ID, -32602, "invalid region capture deadline", err); writeErr != nil {
					return writeErr
				}
				continue
			}
			result, callErr := capturer.CaptureRegion(ctx, request.Request)
			cancel()
			if callErr != nil {
				if writeErr := writeCaptureError(conn, message.ID, "persistent region capture failed", callErr); writeErr != nil {
					return writeErr
				}
				continue
			}
			if err := writeResult(conn, message.ID, regionResult{Result: result}); err != nil {
				return err
			}
		case methodShutdown:
			var request deadlineParams
			if err := decodeStrict(message.Params, &request); err != nil {
				return writeRPCError(conn, message.ID, -32602, "invalid shutdown params", err)
			}
			_, cancel, err := callContext(request.Deadline)
			if err != nil {
				return writeRPCError(conn, message.ID, -32602, "invalid shutdown deadline", err)
			}
			cancel()
			if err := capturer.Close(); err != nil {
				return writeCaptureError(conn, message.ID, "shutdown persistent WGC", err)
			}
			return writeResult(conn, message.ID, shutdownResult{State: "stopped"})
		default:
			if err := writeRPCError(conn, message.ID, -32601, "method not found", errors.New(message.Method)); err != nil {
				return err
			}
		}
	}
}

func callContext(deadline time.Time) (context.Context, context.CancelFunc, error) {
	now := time.Now()
	if deadline.IsZero() || !deadline.After(now) {
		return nil, nil, errors.New("deadline must be in the future")
	}
	if deadline.After(now.Add(MaxCallDuration)) {
		return nil, nil, fmt.Errorf("deadline cannot exceed %s", MaxCallDuration)
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	return ctx, cancel, nil
}

func writeResult(conn *observationprotocol.Conn, id string, result any) error {
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return conn.Write(observationprotocol.Message{ID: id, Result: encoded})
}

func writeCaptureError(conn *observationprotocol.Conn, id, message string, cause error) error {
	data := errorData{Cause: cause.Error()}
	var failure *capture.Error
	if errors.As(cause, &failure) {
		data.Code = failure.Code
	}
	encoded, _ := json.Marshal(data)
	return conn.Write(observationprotocol.Message{ID: id, Error: &observationprotocol.ResponseError{
		Code: -32040, Message: message, Data: encoded,
	}})
}

func writeRPCError(conn *observationprotocol.Conn, id string, code int, message string, cause error) error {
	encoded, _ := json.Marshal(errorData{Cause: cause.Error()})
	return conn.Write(observationprotocol.Message{ID: id, Error: &observationprotocol.ResponseError{
		Code: code, Message: message, Data: encoded,
	}})
}

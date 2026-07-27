// Package observer implements the finite, read-only unified observer call surface.
package observer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/qoli/WindowsAgent/internal/observationapi"
	"github.com/qoli/WindowsAgent/internal/scriptpackage"
)

type BackendResult struct {
	Value           any
	MemoryBytesRead uint64
	FileBytesRead   uint64
}

type Backend interface {
	Call(ctx context.Context, namespace, operation string, arguments map[string]any) (BackendResult, error)
}

type ByteEstimator interface {
	Estimate(namespace, operation string, arguments map[string]any) (memoryBytes, fileBytes uint64, err error)
}

type Session struct {
	jobID       string
	permissions scriptpackage.Permissions
	backend     Backend

	mu          sync.Mutex
	accounting  observationapi.Accounting
	memoryCalls uint32
	fileCalls   uint32
}

func NewSession(jobID string, permissions scriptpackage.Permissions, backend Backend) (*Session, error) {
	if jobID == "" {
		return nil, errors.New("job ID is required")
	}
	if backend == nil {
		return nil, errors.New("backend is required")
	}
	if permissions.Memory == nil && permissions.File == nil {
		return nil, errors.New("at least one permission namespace is required")
	}
	return &Session{jobID: jobID, permissions: permissions, backend: backend}, nil
}

func (s *Session) Call(ctx context.Context, call observationapi.Call) (observationapi.Result, error) {
	if err := call.Validate(); err != nil {
		return observationapi.Result{}, observationapi.NewError(
			"OBSERVER_PROTOCOL_INVALID", "validating-call", call.Namespace, call.Operation, err,
		)
	}
	if call.JobID != s.jobID {
		return observationapi.Result{}, observationapi.NewError(
			"OBSERVER_PROTOCOL_INVALID", "validating-call", call.Namespace, call.Operation, errors.New("jobId does not match initialized session"),
		)
	}
	if err := s.authorize(call.Namespace, call.Operation); err != nil {
		return observationapi.Result{}, err
	}
	var arguments map[string]any
	decoder := json.NewDecoder(bytes.NewReader(call.Arguments))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&arguments); err != nil {
		return observationapi.Result{}, observationapi.NewError(
			"OBSERVER_PROTOCOL_INVALID", "decoding-arguments", call.Namespace, call.Operation, err,
		)
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	s.mu.Lock()
	if estimator, ok := s.backend.(ByteEstimator); ok {
		memoryBytes, fileBytes, err := estimator.Estimate(call.Namespace, call.Operation, arguments)
		if err != nil {
			s.mu.Unlock()
			return observationapi.Result{}, observationapi.NewError(
				"OBSERVER_PROTOCOL_INVALID", "authorizing-call", call.Namespace, call.Operation, err,
			)
		}
		estimated := s.accounting
		if memoryBytes > ^uint64(0)-estimated.MemoryBytesRead ||
			fileBytes > ^uint64(0)-estimated.FileBytesRead {
			s.mu.Unlock()
			return observationapi.Result{}, observationapi.NewError(
				"LIMIT_EXCEEDED", "authorizing-call", call.Namespace, call.Operation, errors.New("estimated byte accounting overflow"),
			)
		}
		estimated.MemoryBytesRead += memoryBytes
		estimated.FileBytesRead += fileBytes
		if err := s.checkLimits(estimated); err != nil {
			s.mu.Unlock()
			return observationapi.Result{}, observationapi.NewError(
				"LIMIT_EXCEEDED", "authorizing-call", call.Namespace, call.Operation, err,
			)
		}
	}
	if err := s.reserveCall(call.Namespace); err != nil {
		s.mu.Unlock()
		return observationapi.Result{}, observationapi.NewError(
			"LIMIT_EXCEEDED", "authorizing-call", call.Namespace, call.Operation, err,
		)
	}
	s.mu.Unlock()

	result, err := s.backend.Call(ctx, call.Namespace, call.Operation, arguments)
	if err != nil {
		var typed *observationapi.Error
		if errors.As(err, &typed) {
			return observationapi.Result{}, typed
		}
		return observationapi.Result{}, observationapi.NewError(
			"OBSERVER_CALL_FAILED", "executing-call", call.Namespace, call.Operation, err,
		)
	}
	value, err := json.Marshal(result.Value)
	if err != nil {
		return observationapi.Result{}, observationapi.NewError(
			"OBSERVER_PROTOCOL_INVALID", "encoding-result", call.Namespace, call.Operation, err,
		)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.accounting
	next.CallsUsed++
	if result.MemoryBytesRead > ^uint64(0)-next.MemoryBytesRead ||
		result.FileBytesRead > ^uint64(0)-next.FileBytesRead {
		return observationapi.Result{}, observationapi.NewError(
			"LIMIT_EXCEEDED", "accounting-call", call.Namespace, call.Operation, errors.New("byte accounting overflow"),
		)
	}
	next.MemoryBytesRead += result.MemoryBytesRead
	next.FileBytesRead += result.FileBytesRead
	if err := s.checkLimits(next); err != nil {
		return observationapi.Result{}, observationapi.NewError(
			"LIMIT_EXCEEDED", "accounting-call", call.Namespace, call.Operation, err,
		)
	}
	s.accounting = next
	return observationapi.Result{
		JobID:          call.JobID,
		ObserverCallID: call.ObserverCallID,
		Namespace:      call.Namespace,
		Operation:      call.Operation,
		ObservedAt:     time.Now().UTC(),
		Accounting:     next,
		Value:          value,
	}, nil
}

func (s *Session) authorize(namespace, operation string) error {
	var allowed []string
	switch namespace {
	case observationapi.NamespaceMemory:
		if s.permissions.Memory != nil {
			allowed = s.permissions.Memory.Operations
		}
	case observationapi.NamespaceFile:
		if s.permissions.File != nil {
			allowed = s.permissions.File.Operations
		}
	}
	if !slices.Contains(allowed, operation) {
		return observationapi.NewError(
			"PERMISSION_DENIED", "authorizing-call", namespace, operation, errors.New("operation is absent from the effective permission set"),
		)
	}
	return nil
}

func (s *Session) checkLimits(accounting observationapi.Accounting) error {
	if permission := s.permissions.Memory; permission != nil {
		if accounting.MemoryBytesRead > permission.MaxBytesRead {
			return fmt.Errorf("memory bytes %d exceed limit %d", accounting.MemoryBytesRead, permission.MaxBytesRead)
		}
	}
	if permission := s.permissions.File; permission != nil {
		if accounting.FileBytesRead > permission.MaxBytesRead {
			return fmt.Errorf("file bytes %d exceed limit %d", accounting.FileBytesRead, permission.MaxBytesRead)
		}
	}
	return nil
}

func (s *Session) reserveCall(namespace string) error {
	switch namespace {
	case observationapi.NamespaceMemory:
		if s.permissions.Memory == nil || s.memoryCalls >= s.permissions.Memory.MaxCalls {
			return errors.New("memory call limit exceeded")
		}
		s.memoryCalls++
	case observationapi.NamespaceFile:
		if s.permissions.File == nil || s.fileCalls >= s.permissions.File.MaxCalls {
			return errors.New("file call limit exceeded")
		}
		s.fileCalls++
	default:
		return fmt.Errorf("unsupported namespace %q", namespace)
	}
	return nil
}

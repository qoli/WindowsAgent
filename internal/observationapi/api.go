package observationapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/strictjson"
)

func ValidateBlobHandle(value string) error {
	if len(value) != 64 || strings.ToLower(value) != value {
		return errors.New("blob handle must be 64 lowercase hexadecimal characters")
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return errors.New("blob handle must be hexadecimal")
		}
	}
	return nil
}

const ProtocolVersion = "2026-08-07"

const (
	NamespaceMemory = "memory"
	NamespaceFile   = "file"
	NamespaceScreen = "screen"
)

var operations = map[string][]string{
	NamespaceMemory: {"modules", "regions", "scan", "resolveRip", "readBatch", "readStrided"},
	NamespaceFile:   {"list", "stat", "read", "hash", "openBlob"},
	NamespaceScreen: {"readRegion"},
}

func OperationAllowed(namespace, operation string) bool {
	return slices.Contains(operations[namespace], operation)
}

type Call struct {
	JobID          string          `json:"jobId"`
	ObserverCallID string          `json:"observerCallId"`
	Namespace      string          `json:"namespace"`
	Operation      string          `json:"operation"`
	Arguments      json.RawMessage `json:"arguments"`
}

func (c Call) Validate() error {
	if c.JobID == "" {
		return errors.New("jobId is required")
	}
	if c.ObserverCallID == "" {
		return errors.New("observerCallId is required")
	}
	if _, ok := operations[c.Namespace]; !ok {
		return fmt.Errorf("unsupported observer namespace %q", c.Namespace)
	}
	if !OperationAllowed(c.Namespace, c.Operation) {
		return fmt.Errorf("unsupported %s operation %q", c.Namespace, c.Operation)
	}
	if len(c.Arguments) == 0 || !json.Valid(c.Arguments) {
		return errors.New("arguments must be valid JSON")
	}
	if err := strictjson.Validate(c.Arguments); err != nil {
		return fmt.Errorf("arguments must be strict JSON: %w", err)
	}
	return nil
}

type Accounting struct {
	CallsUsed        uint32 `json:"callsUsed"`
	MemoryBytesRead  uint64 `json:"memoryBytesRead"`
	FileBytesRead    uint64 `json:"fileBytesRead"`
	ScreenPixelsRead uint64 `json:"screenPixelsRead"`
}

type Result struct {
	JobID          string          `json:"jobId"`
	ObserverCallID string          `json:"observerCallId"`
	Namespace      string          `json:"namespace"`
	Operation      string          `json:"operation"`
	ObservedAt     time.Time       `json:"observedAt"`
	Accounting     Accounting      `json:"accounting"`
	Value          json.RawMessage `json:"value"`
}

type Error struct {
	Kind      string `json:"kind"`
	Stage     string `json:"stage"`
	Namespace string `json:"namespace,omitempty"`
	Operation string `json:"operation,omitempty"`
	Retryable bool   `json:"retryable"`
	Cause     error  `json:"-"`
}

func (e *Error) Error() string {
	if e.Cause == nil {
		return e.Kind
	}
	return fmt.Sprintf("%s at %s: %v", e.Kind, e.Stage, e.Cause)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func NewError(kind, stage, namespace, operation string, cause error) *Error {
	return &Error{
		Kind:      kind,
		Stage:     stage,
		Namespace: namespace,
		Operation: operation,
		Retryable: false,
		Cause:     cause,
	}
}

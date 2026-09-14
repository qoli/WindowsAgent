// Package processinventory exposes one read-only snapshot of Windows processes
// and services using osquery-aligned field names and relationship semantics.
package processinventory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

const RuntimeID = "windows-process-inventory-v1"
const SchemaVersion uint32 = 1

type Process struct {
	PID       uint32  `json:"pid"`
	Parent    uint32  `json:"parent"`
	Name      string  `json:"name"`
	Path      *string `json:"path"`
	Cmdline   *string `json:"cmdline"`
	State     *string `json:"state"`
	StartTime *int64  `json:"start_time"`
	SessionID *uint32 `json:"session_id"`
	Threads   uint32  `json:"threads"`
}

type Service struct {
	// PID is zero when SCM does not report a current hosting process. Only a
	// nonzero PID identifies the Process row with the same PID.
	PID                 uint32  `json:"pid"`
	Name                string  `json:"name"`
	DisplayName         string  `json:"display_name"`
	Status              string  `json:"status"`
	ServiceType         string  `json:"service_type"`
	StartType           *string `json:"start_type"`
	Path                *string `json:"path"`
	ModulePath          *string `json:"module_path"`
	Description         *string `json:"description"`
	UserAccount         *string `json:"user_account"`
	Win32ExitCode       uint32  `json:"win32_exit_code"`
	ServiceSpecificCode uint32  `json:"service_exit_code"`
}

type Snapshot struct {
	SchemaVersion uint32    `json:"schemaVersion"`
	Runtime       string    `json:"runtime"`
	ObservedAt    time.Time `json:"observedAt"`
	Processes     []Process `json:"processes"`
	Services      []Service `json:"services"`
}

type Collector interface {
	Snapshot(context.Context) (Snapshot, error)
}

type Error struct {
	Code  string
	Stage string
	Cause error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s at %s: %v", e.Code, e.Stage, e.Cause)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type source interface {
	Processes(context.Context) ([]Process, error)
	Services(context.Context) ([]Service, error)
}

type OSCollector struct {
	source source
	now    func() time.Time
}

func NewOSCollector() OSCollector {
	return OSCollector{source: osSource{}, now: time.Now}
}

func (c OSCollector) Snapshot(ctx context.Context) (Snapshot, error) {
	if ctx == nil {
		return Snapshot{}, &Error{Code: "process_inventory_context_required", Stage: "validating-request", Cause: errors.New("context is required")}
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, &Error{Code: "process_inventory_canceled", Stage: "validating-request", Cause: err}
	}
	if c.source == nil || c.now == nil {
		return Snapshot{}, &Error{Code: "process_inventory_not_initialized", Stage: "validating-runtime", Cause: errors.New("collector is not initialized")}
	}
	processes, err := c.source.Processes(ctx)
	if err != nil {
		return Snapshot{}, &Error{Code: "process_inventory_process_list_failed", Stage: "enumerating-processes", Cause: err}
	}
	if len(processes) == 0 {
		return Snapshot{}, &Error{Code: "process_inventory_process_list_empty", Stage: "enumerating-processes", Cause: errors.New("Windows returned no process rows")}
	}
	services, err := c.source.Services(ctx)
	if err != nil {
		return Snapshot{}, &Error{Code: "process_inventory_service_list_failed", Stage: "enumerating-services", Cause: err}
	}
	sort.Slice(processes, func(i, j int) bool { return processes[i].PID < processes[j].PID })
	sort.Slice(services, func(i, j int) bool {
		if services[i].PID != services[j].PID {
			return services[i].PID < services[j].PID
		}
		return services[i].Name < services[j].Name
	})
	return Snapshot{
		SchemaVersion: SchemaVersion,
		Runtime:       RuntimeID,
		ObservedAt:    c.now().UTC(),
		Processes:     processes,
		Services:      services,
	}, nil
}

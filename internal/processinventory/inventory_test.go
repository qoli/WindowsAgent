package processinventory

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeSource struct {
	processes  []Process
	services   []Service
	processErr error
	serviceErr error
}

func (f fakeSource) Processes(context.Context) ([]Process, error) {
	return f.processes, f.processErr
}

func (f fakeSource) Services(context.Context) ([]Service, error) {
	return f.services, f.serviceErr
}

func TestSnapshotReturnsSortedOsqueryShapedTables(t *testing.T) {
	observedAt := time.Date(2026, 9, 15, 3, 0, 0, 0, time.FixedZone("test", 8*60*60))
	collector := OSCollector{
		source: fakeSource{
			processes: []Process{{PID: 20, Name: "child.exe", Parent: 10}, {PID: 10, Name: "parent.exe"}},
			services: []Service{
				{PID: 20, Name: "z-service", Status: "RUNNING", ServiceType: "OWN_PROCESS"},
				{PID: 20, Name: "a-service", Status: "RUNNING", ServiceType: "OWN_PROCESS"},
			},
		},
		now: func() time.Time { return observedAt },
	}

	snapshot, err := collector.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SchemaVersion != SchemaVersion || snapshot.Runtime != RuntimeID || !snapshot.ObservedAt.Equal(observedAt.UTC()) {
		t.Fatalf("snapshot metadata = %+v", snapshot)
	}
	if len(snapshot.Processes) != 2 || snapshot.Processes[0].PID != 10 || snapshot.Processes[1].PID != 20 {
		t.Fatalf("process order = %+v", snapshot.Processes)
	}
	if len(snapshot.Services) != 2 || snapshot.Services[0].Name != "a-service" || snapshot.Services[1].Name != "z-service" {
		t.Fatalf("service order = %+v", snapshot.Services)
	}
}

func TestSnapshotFailsWhenProcessEnumerationFails(t *testing.T) {
	cause := errors.New("snapshot unavailable")
	collector := OSCollector{source: fakeSource{processErr: cause}, now: time.Now}
	_, err := collector.Snapshot(context.Background())
	var inventoryError *Error
	if !errors.As(err, &inventoryError) || inventoryError.Code != "process_inventory_process_list_failed" || !errors.Is(err, cause) {
		t.Fatalf("error = %v", err)
	}
}

func TestSnapshotFailsWhenServiceEnumerationFails(t *testing.T) {
	cause := errors.New("SCM unavailable")
	collector := OSCollector{
		source: fakeSource{processes: []Process{{PID: 4, Name: "System"}}, serviceErr: cause},
		now:    time.Now,
	}
	_, err := collector.Snapshot(context.Background())
	var inventoryError *Error
	if !errors.As(err, &inventoryError) || inventoryError.Code != "process_inventory_service_list_failed" || !errors.Is(err, cause) {
		t.Fatalf("error = %v", err)
	}
}

func TestSnapshotFailsForEmptyProcessTable(t *testing.T) {
	collector := OSCollector{source: fakeSource{}, now: time.Now}
	_, err := collector.Snapshot(context.Background())
	var inventoryError *Error
	if !errors.As(err, &inventoryError) || inventoryError.Code != "process_inventory_process_list_empty" {
		t.Fatalf("error = %v", err)
	}
}

package watchdog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxHealthResponseBytes = 4 << 10

type ProcessInfo struct {
	PID       uint32
	SessionID uint32
}

type ProcessInspector interface {
	FindByExecutablePath(context.Context, string) ([]ProcessInfo, error)
}

type TargetObserver struct {
	httpClient       *http.Client
	processInspector ProcessInspector
}

func NewTargetObserver(httpClient *http.Client, processInspector ProcessInspector) (*TargetObserver, error) {
	if httpClient == nil || processInspector == nil {
		return nil, fmt.Errorf("watchdog HTTP client and process inspector are required")
	}
	return &TargetObserver{httpClient: httpClient, processInspector: processInspector}, nil
}

func (o *TargetObserver) Observe(ctx context.Context, target Target) (Observation, error) {
	data := make(map[string]any, len(target.Probes))
	for index, probe := range target.Probes {
		var observation Observation
		var err error
		switch probe.Type {
		case "process":
			observation, err = o.observeProcess(ctx, probe)
		case "http-json":
			observation, err = o.observeHTTP(ctx, probe)
		default:
			return Observation{}, fmt.Errorf("target %s has unsupported probe type %q", target.ID, probe.Type)
		}
		if err != nil {
			return Observation{}, fmt.Errorf("target %s probe %d (%s): %w", target.ID, index, probe.Type, err)
		}
		data[fmt.Sprintf("probe%d", index)] = observation.Data
		if !observation.Healthy {
			return Observation{Healthy: false, Detail: observation.Detail, Data: data}, nil
		}
	}
	return Observation{Healthy: true, Detail: "all configured probes are healthy", Data: data}, nil
}

func (o *TargetObserver) observeProcess(ctx context.Context, probe ProbeConfig) (Observation, error) {
	processes, err := o.processInspector.FindByExecutablePath(ctx, probe.ExecutablePath)
	if err != nil {
		return Observation{}, err
	}
	if len(processes) == 0 {
		return Observation{Healthy: false, Detail: "configured executable process is absent",
			Data: map[string]any{"executablePath": probe.ExecutablePath, "processCount": 0}}, nil
	}
	if len(processes) != 1 {
		return Observation{Healthy: false, Detail: "configured executable has multiple running processes",
			Data: map[string]any{"executablePath": probe.ExecutablePath, "processCount": len(processes)}}, nil
	}
	process := processes[0]
	if *probe.RequireInteractiveSession && process.SessionID == 0 {
		return Observation{Healthy: false, Detail: "configured executable is running in Session 0",
			Data: map[string]any{"executablePath": probe.ExecutablePath, "pid": process.PID, "sessionId": process.SessionID}}, nil
	}
	return Observation{Healthy: true, Detail: "configured executable has one matching process",
		Data: map[string]any{"executablePath": probe.ExecutablePath, "pid": process.PID, "sessionId": process.SessionID}}, nil
}

func (o *TargetObserver) observeHTTP(ctx context.Context, probe ProbeConfig) (Observation, error) {
	requestContext, cancel := context.WithTimeout(ctx, time.Duration(probe.TimeoutMS)*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, probe.URL, nil)
	if err != nil {
		return Observation{}, fmt.Errorf("create health request: %w", err)
	}
	response, err := o.httpClient.Do(request)
	if err != nil {
		return Observation{Healthy: false, Detail: "configured HTTP health request failed",
			Data: map[string]any{"url": probe.URL, "error": err.Error()}}, nil
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxHealthResponseBytes+1))
	if err != nil {
		return Observation{}, fmt.Errorf("read health response: %w", err)
	}
	if len(body) > maxHealthResponseBytes {
		return Observation{Healthy: false, Detail: "configured HTTP health response exceeds bound",
			Data: map[string]any{"url": probe.URL, "statusCode": response.StatusCode}}, nil
	}
	if response.StatusCode != probe.ExpectedStatusCode {
		return Observation{Healthy: false, Detail: "configured HTTP health status code does not match",
			Data: map[string]any{"url": probe.URL, "statusCode": response.StatusCode}}, nil
	}
	var payload struct {
		Status string `json:"status"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return Observation{Healthy: false, Detail: "configured HTTP health response is not the strict status contract",
			Data: map[string]any{"url": probe.URL, "statusCode": response.StatusCode, "error": err.Error()}}, nil
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return Observation{Healthy: false, Detail: "configured HTTP health response contains multiple JSON values",
			Data: map[string]any{"url": probe.URL, "statusCode": response.StatusCode}}, nil
	} else if err != io.EOF {
		return Observation{Healthy: false, Detail: "configured HTTP health response has invalid trailing data",
			Data: map[string]any{"url": probe.URL, "statusCode": response.StatusCode, "error": err.Error()}}, nil
	}
	if payload.Status != probe.ExpectedJSONStatus {
		return Observation{Healthy: false, Detail: "configured HTTP health JSON status does not match",
			Data: map[string]any{"url": probe.URL, "statusCode": response.StatusCode, "status": payload.Status}}, nil
	}
	return Observation{Healthy: true, Detail: "configured HTTP health response matches",
		Data: map[string]any{"url": probe.URL, "statusCode": response.StatusCode, "status": payload.Status}}, nil
}

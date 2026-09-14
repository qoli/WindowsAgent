package delegatedhttp

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/delegatedtask"
	"github.com/qoli/WindowsAgent/internal/eventstream"
)

const testToken = "0123456789abcdef0123456789abcdef"

type fakeTasks struct {
	mu         sync.Mutex
	task       delegatedtask.Task
	streamSent bool
}

func (tasks *fakeTasks) Submit(_ context.Context, request delegatedtask.SubmitRequest) (delegatedtask.Task, error) {
	tasks.task = delegatedtask.Task{TaskID: "task_1", State: delegatedtask.StateRunning, CWD: request.CWD, LastSequence: 2}
	return tasks.task, nil
}
func (tasks *fakeTasks) Get(id string) (delegatedtask.Task, error) {
	if id != tasks.task.TaskID {
		return delegatedtask.Task{}, delegatedtask.ErrTaskNotFound
	}
	return tasks.task, nil
}
func (tasks *fakeTasks) Send(context.Context, string, delegatedtask.MessageRequest) (delegatedtask.Task, error) {
	return tasks.task, nil
}
func (tasks *fakeTasks) Cancel(context.Context, string) (delegatedtask.Task, error) {
	return tasks.task, nil
}
func (tasks *fakeTasks) Events(context.Context, string, uint64, int) ([]eventstream.Event, uint64, uint64, error) {
	return []eventstream.Event{{Sequence: 2, Type: "task.started"}}, 2, 2, nil
}
func (tasks *fakeTasks) WaitEvents(ctx context.Context, _ string, _ uint64) ([]eventstream.Event, error) {
	tasks.mu.Lock()
	if !tasks.streamSent {
		tasks.streamSent = true
		tasks.mu.Unlock()
		return []eventstream.Event{{Sequence: 2, Type: "task.started"}}, nil
	}
	tasks.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestServerRequiresAuthenticationAndCreatesTask(t *testing.T) {
	tasks := &fakeTasks{}
	server, err := New(tasks, testToken)
	if err != nil {
		t.Fatal(err)
	}
	unauthorized := httptest.NewRequest(http.MethodPost, "/v1/delegated-tasks", strings.NewReader(`{"prompt":"work","cwd":"C:\\work"}`))
	unauthorizedResult := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResult, unauthorized)
	if unauthorizedResult.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorizedResult.Code)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/delegated-tasks", strings.NewReader(`{"prompt":"work","cwd":"C:\\work"}`))
	request.Header.Set("Authorization", "Bearer "+testToken)
	result := httptest.NewRecorder()
	server.Handler().ServeHTTP(result, request)
	if result.Code != http.StatusCreated || result.Header().Get("Location") != "/v1/delegated-tasks/task_1" {
		t.Fatalf("status=%d location=%q body=%s", result.Code, result.Header().Get("Location"), result.Body.String())
	}
}

func TestServerRejectsUnknownRequestFields(t *testing.T) {
	server, err := New(&fakeTasks{}, testToken)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/delegated-tasks", strings.NewReader(`{"prompt":"work","cwd":"C:\\work","fallback":true}`))
	request.Header.Set("Authorization", "Bearer "+testToken)
	result := httptest.NewRecorder()
	server.Handler().ServeHTTP(result, request)
	if result.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestServerStreamsTaskEventsAsNDJSON(t *testing.T) {
	tasks := &fakeTasks{task: delegatedtask.Task{TaskID: "task_1", State: delegatedtask.StateRunning}}
	api, err := New(tasks, testToken)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/delegated-tasks/task_1/events/stream?after=0", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var event eventstream.Event
	if err := json.NewDecoder(bufio.NewReader(response.Body)).Decode(&event); err != nil {
		t.Fatal(err)
	}
	if event.Sequence != 2 || event.Type != "task.started" {
		t.Fatalf("event = %+v", event)
	}
}

func TestServerReturnsTaskNotFound(t *testing.T) {
	server, err := New(&fakeTasks{}, testToken)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/delegated-tasks/missing", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	result := httptest.NewRecorder()
	server.Handler().ServeHTTP(result, request)
	if result.Code != http.StatusNotFound {
		t.Fatalf("status = %d", result.Code)
	}
	var envelope errorEnvelope
	if err := json.Unmarshal(result.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "task_not_found" {
		t.Fatalf("error = %+v", envelope.Error)
	}
}

package actionosd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/actionrun"
	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/streamaction"
)

func TestModelShowsCurrentActionAndLatestThreeDistinctActivities(t *testing.T) {
	model := &Model{}
	start := time.Date(2026, 8, 8, 1, 2, 3, 0, time.UTC)
	if err := model.Apply(testEvent("action.started", start, `{"state":"RUNNING","actionId":"game/leave","lifecycle":"linear","interruptible":true}`)); err != nil {
		t.Fatal(err)
	}
	for index, message := range []string{"Waiting for launch", "Waiting for launch", "Launch detected", "Throttle set to 100%", "Mass Lock released"} {
		payload, _ := json.Marshal(map[string]string{"message": message, "level": "info"})
		if err := model.Apply(testEvent(streamaction.ActivityEventType, start.Add(time.Duration(index+1)*time.Second), string(payload))); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := model.Snapshot(start.Add(5 * time.Second))
	if !snapshot.Visible || snapshot.Status != StatusLive || snapshot.ActionID != "game/leave" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if len(snapshot.Activities) != 3 || snapshot.Activities[0].Message != "Launch detected" || snapshot.Activities[2].Message != "Mass Lock released" {
		t.Fatalf("activities = %+v", snapshot.Activities)
	}
}

func TestModelTerminalVisibilityExpires(t *testing.T) {
	model := &Model{}
	start := time.Now().UTC()
	if err := model.Apply(testEvent("action.started", start, `{"state":"RUNNING","actionId":"game/leave","lifecycle":"linear","interruptible":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := model.Apply(testEvent("action.completed", start.Add(time.Second), `{"state":"COMPLETED","output":{}}`)); err != nil {
		t.Fatal(err)
	}
	if !model.Snapshot(start.Add(2 * time.Second)).Visible {
		t.Fatal("completed Action disappeared before terminal visibility elapsed")
	}
	if model.Snapshot(start.Add(5 * time.Second)).Visible {
		t.Fatal("completed Action remained visible after terminal visibility elapsed")
	}
}

func TestModelReconstructsRunningActionFromRecentDomainEvent(t *testing.T) {
	model := &Model{}
	observed := time.Now().UTC()
	event := testEvent("action.game.update", observed, `{"phase":"WAITING"}`)
	if err := model.Apply(event); err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot(observed.Add(time.Minute))
	if !snapshot.Visible || snapshot.Status != StatusLive || snapshot.ActionID != "game/leave" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestModelRejectsMalformedActivityWithoutDisplayFallback(t *testing.T) {
	model := &Model{}
	err := model.Apply(testEvent(streamaction.ActivityEventType, time.Now().UTC(), `{"message":"","level":"info"}`))
	if err == nil || !strings.Contains(err.Error(), "canonical non-empty line") {
		t.Fatalf("error = %v", err)
	}
	if model.Snapshot(time.Now()).Visible {
		t.Fatal("malformed activity produced visible fallback state")
	}
}

func testEvent(eventType string, observedAt time.Time, payload string) eventstream.Event {
	return eventstream.Event{
		Stream: actionrun.StreamName, Type: eventType, ObservedAt: observedAt, CommittedAt: observedAt,
		CorrelationID: "act_fixture", Source: eventstream.Source{ModuleID: "game/leave"},
		Payload: json.RawMessage(payload),
	}
}

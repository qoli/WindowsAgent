package actionosd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/actionrun"
	"github.com/qoli/WindowsAgent/internal/actionsequence"
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

func TestModelProjectsActionSequenceAsOneParentSession(t *testing.T) {
	model := &Model{}
	start := time.Date(2026, 8, 10, 1, 2, 3, 0, time.UTC)
	if err := model.Apply(sequenceEvent("action.started", start, `{"state":"RUNNING","actionId":"system/ephemeral-action-sequence","lifecycle":"linear","interruptible":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := model.Apply(sequenceEvent(actionsequence.EventStarted, start.Add(time.Millisecond), `{"stepCount":2}`)); err != nil {
		t.Fatal(err)
	}
	if err := model.Apply(sequenceEvent(actionsequence.EventStepStarted, start.Add(time.Second), `{"step":1,"totalSteps":2,"actionId":"game/align","childExecutionId":"act_child_1","completion":"stream"}`)); err != nil {
		t.Fatal(err)
	}
	if err := model.Apply(sequenceEvent(actionsequence.EventChildEvent, start.Add(2*time.Second), `{"step":1,"actionId":"game/align","childExecutionId":"act_child_1","type":"action.activity","payload":{"message":"Aligning target","level":"info"}}`)); err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot(start.Add(2 * time.Second))
	if snapshot.InvocationID != "act_sequence" || snapshot.ActionID != "game/align" || snapshot.Status != StatusLive || len(snapshot.Activities) != 2 ||
		snapshot.Activities[0].Message != "Step 1/2" || snapshot.Activities[1].Message != "Aligning target" {
		t.Fatalf("step one snapshot = %+v", snapshot)
	}
	if err := model.Apply(sequenceEvent(actionsequence.EventChildOutput, start.Add(3*time.Second), `{"step":1,"actionId":"game/align","childExecutionId":"act_child_1","output":{"aligned":true}}`)); err != nil {
		t.Fatal(err)
	}
	if err := model.Apply(sequenceEvent(actionsequence.EventStepCompleted, start.Add(4*time.Second), `{"step":1,"totalSteps":2,"actionId":"game/align","childExecutionId":"act_child_1"}`)); err != nil {
		t.Fatal(err)
	}
	if err := model.Apply(sequenceEvent(actionsequence.EventStepStarted, start.Add(5*time.Second), `{"step":2,"totalSteps":2,"actionId":"game/throttle","childExecutionId":"act_child_2","completion":"return"}`)); err != nil {
		t.Fatal(err)
	}
	snapshot = model.Snapshot(start.Add(5 * time.Second))
	if snapshot.ActionID != "game/throttle" || len(snapshot.Activities) != 1 || snapshot.Activities[0].Message != "Step 2/2" {
		t.Fatalf("step two snapshot = %+v", snapshot)
	}
	if err := model.Apply(sequenceEvent("action.completed", start.Add(6*time.Second), `{"state":"COMPLETED","output":{"completedSteps":2}}`)); err != nil {
		t.Fatal(err)
	}
	if snapshot = model.Snapshot(start.Add(7 * time.Second)); snapshot.Status != StatusDone || snapshot.ActionID != "game/throttle" {
		t.Fatalf("terminal snapshot = %+v", snapshot)
	}
}

func TestModelRejectsSequenceEventsWithoutExactParentAndProvenance(t *testing.T) {
	model := &Model{}
	err := model.Apply(sequenceEvent(actionsequence.EventStepStarted, time.Now().UTC(), `{"step":1,"totalSteps":1,"actionId":"game/a","childExecutionId":"act_child","completion":"return"}`))
	if err == nil || !strings.Contains(err.Error(), "current OSD invocation") {
		t.Fatalf("missing parent error = %v", err)
	}
	if err := model.Apply(sequenceEvent("action.started", time.Now().UTC(), `{"state":"RUNNING","actionId":"system/ephemeral-action-sequence","lifecycle":"linear","interruptible":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := model.Apply(sequenceEvent(actionsequence.EventStarted, time.Now().UTC(), `{"stepCount":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := model.Apply(sequenceEvent("action.sequence.unknown", time.Now().UTC(), `{}`)); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unknown event error = %v", err)
	}
}

func testEvent(eventType string, observedAt time.Time, payload string) eventstream.Event {
	return eventstream.Event{
		Stream: actionrun.StreamName, Type: eventType, ObservedAt: observedAt, CommittedAt: observedAt,
		CorrelationID: "act_fixture", Source: eventstream.Source{ModuleID: "game/leave"},
		Payload: json.RawMessage(payload),
	}
}

func sequenceEvent(eventType string, observedAt time.Time, payload string) eventstream.Event {
	return eventstream.Event{
		Stream: actionrun.StreamName, Type: eventType, ObservedAt: observedAt, CommittedAt: observedAt,
		CorrelationID: "act_sequence", Source: eventstream.Source{ModuleID: actionsequence.ActionID},
		Payload: json.RawMessage(payload),
	}
}

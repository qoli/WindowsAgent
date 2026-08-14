package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

type flightStatusCompositeCaller struct {
	calls []string
}

func (c *flightStatusCompositeCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	c.calls = append(c.calls, id)
	switch id {
	case "elite-dangerous/flight-prompt-text":
		if len(inputs) != 0 {
			return nil, errors.New("flight-prompt-text inputs were not empty")
		}
		return json.RawMessage(`{
		  "schemaVersion":2,"text":"THROTTLE UP TO ENGAGE","confidence":0.99,"decoding":{},"evidence":{},"model":{},"timing":{},
		  "cascade":{"policy":"EXPLICIT_PERFORMANCE_FIRST","selectedRoute":"REFERENCE_RAW_RGB","terminalReason":"primary-accepted","attemptCount":1,"gate":null,"transitions":[],
		    "attempts":[{"routeId":"REFERENCE_RAW_RGB","text":"THROTTLE UP TO ENGAGE","confidence":0.99,"timing":{},"decision":{"routeDecision":{"accepted":true,"state":"FSD_THROTTLE_UP_REQUIRED"}}}],
		    "selectedDecision":{"schemaVersion":1,"routeDecision":{"accepted":true,"state":"FSD_THROTTLE_UP_REQUIRED"},"flightStatus":{"state":"FSD_THROTTLE_UP_REQUIRED","known":true},"source":{"text":"THROTTLE UP TO ENGAGE","normalizedText":"THROTTLEUPTOENGAGE","ocrConfidence":0.99},"decision":{"accepted":true,"confidence":0.99,"threshold":0.3,"margin":0.5,"marginThreshold":0.1,"similarityThreshold":0.6,"candidateState":"FSD_THROTTLE_UP_REQUIRED","candidateAlias":"THROTTLE UP TO ENGAGE","textSimilarity":1.0,"runnerUpState":"FSD_CHARGING","runnerUpConfidence":0.49}}
		  }
		}`), nil
	default:
		return nil, errors.New("unexpected flight-status child Action: " + id)
	}
}

func TestEliteFlightStatusOwnsFreshOCRToSemanticPipeline(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "flight-status"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	caller := &flightStatusCompositeCaller{}
	output, err := (Runner{}).Run(context.Background(), pkg, map[string]any{}, caller, &fixtureReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(caller.calls, []string{"elite-dangerous/flight-prompt-text"}) {
		t.Fatalf("child calls = %v", caller.calls)
	}
	if !contains(string(output), `"state":"FSD_THROTTLE_UP_REQUIRED"`) || !contains(string(output), `"text":"THROTTLE UP TO ENGAGE"`) {
		t.Fatalf("output = %s", output)
	}
}

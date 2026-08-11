package visuallog

import (
	"strings"
	"testing"
)

func TestParseConfigAcceptsEliteDescriptionContract(t *testing.T) {
	config, err := ParseConfig([]byte(validConfigJSON()))
	if err != nil {
		t.Fatal(err)
	}
	if config.Runtime != RuntimeID || config.TargetExecutable != "EliteDangerous64.exe" || config.WarmupCalls != 1 {
		t.Fatalf("unexpected config: %+v", config)
	}
	if !strings.Contains(strings.ToLower(config.Prompt), "do not infer actions or events") {
		t.Fatalf("prompt does not preserve the tested contract: %q", config.Prompt)
	}
}

func TestParseConfigRejectsUnknownMissingAndInvalidState(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"unknown", strings.Replace(validConfigJSON(), `"warmupCalls":1`, `"warmupCalls":1,"fallbackModel":"other"`, 1), "unknown field"},
		{"missing prompt", strings.Replace(validConfigJSON(), `"prompt":"Describe the directly visible physical scene in this single Elite Dangerous screenshot using 8-16 words. Mention the environment and large structures behind the cockpit overlay, not the gameplay situation. Ignore HUD text and do not infer actions or events."`, `"prompt":""`, 1), "prompt is required"},
		{"stale bound", strings.Replace(validConfigJSON(), `"maxFrameAgeMs":3000`, `"maxFrameAgeMs":0`, 1), "maxFrameAgeMs"},
		{"warmup bound", strings.Replace(validConfigJSON(), `"warmupFrameTimeoutMs":5000`, `"warmupFrameTimeoutMs":0`, 1), "warmupFrameTimeoutMs"},
		{"no warmup", strings.Replace(validConfigJSON(), `"warmupCalls":1`, `"warmupCalls":0`, 1), "warmupCalls"},
		{"duplicate", strings.Replace(validConfigJSON(), `"intervalMs":2000`, `"intervalMs":2000,"intervalMs":3000`, 1), "duplicate"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(test.body))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func validConfigJSON() string {
	return `{
  "schemaVersion":2,
  "moduleId":"elite-dangerous/visual-log",
  "kind":"visual-log",
  "runtime":"omlx-visual-log-v2",
  "targetExecutable":"EliteDangerous64.exe",
  "intervalMs":2000,
  "warmupCalls":1,
  "evidence":{"frameTapName":"Local\\WindowsAgent.Evidence.EliteDangerous.v1","maxFrameAgeMs":3000,"warmupFrameTimeoutMs":5000},
  "prompt":"Describe the directly visible physical scene in this single Elite Dangerous screenshot using 8-16 words. Mention the environment and large structures behind the cockpit overlay, not the gameplay situation. Ignore HUD text and do not infer actions or events.",
  "model":{"id":"gemma-4-e4b-it-8bit","maxTokens":64,"temperature":1.0,"topP":0.95,"topK":64},
  "output":{"stream":"visual-log","observationType":"visual-log.observation","failureType":"visual-log.failure","descriptionMinWords":8,"descriptionMaxWords":16}
}`
}

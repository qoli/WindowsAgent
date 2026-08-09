package watchdog

import (
	"strings"
	"testing"
)

func TestParseConfigAcceptsStrictOneWayTargetContract(t *testing.T) {
	config, err := ParseConfig([]byte(validConfigJSON()))
	if err != nil {
		t.Fatal(err)
	}
	if config.SchemaVersion != 1 || len(config.Targets) != 1 || config.Targets[0].ID != "event-stream" {
		t.Fatalf("config = %+v", config)
	}
}

func TestParseConfigRejectsUnknownAndMissingRequiredState(t *testing.T) {
	for name, test := range map[string]struct {
		body string
		want string
	}{
		"unknown field":                         {strings.Replace(validConfigJSON(), `"checkIntervalMs":1000`, `"checkIntervalMs":1000,"fallback":true`, 1), "unknown field"},
		"missing explicit process session rule": {strings.Replace(validConfigJSON(), `,"requireInteractiveSession":true`, "", 1), "requireInteractiveSession is required"},
		"remote health URL":                     {strings.Replace(validConfigJSON(), "127.0.0.1", "192.0.2.1", 1), "explicit loopback"},
		"missing recovery owner":                {strings.Replace(validConfigJSON(), `"expectedTaskDescription":"owned event stream"`, `"expectedTaskDescription":""`, 1), "expectedTaskDescription"},
		"unknown probe":                         {strings.Replace(validConfigJSON(), `"type":"process"`, `"type":"pid-guess"`, 1), "unsupported probe type"},
		"wrong-shape zero field":                {strings.Replace(validConfigJSON(), `"requireInteractiveSession":true`, `"requireInteractiveSession":true,"timeoutMs":0`, 1), "does not allow field"},
		"implicit desired state":                {strings.Replace(validConfigJSON(), `"desiredState":"running",`, "", 1), "desiredState must be explicitly set"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseConfig([]byte(test.body))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestConfigOrdersStartupDependenciesAndRejectsCycles(t *testing.T) {
	config, err := ParseConfig([]byte(validConfigJSON()))
	if err != nil {
		t.Fatal(err)
	}
	capture := config.Targets[0]
	capture.ID = "capture-agent"
	capture.StartAfterHealthy = []string{"event-stream"}
	config.Targets = []Target{capture, config.Targets[0]}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	ordered, err := config.StartupOrder()
	if err != nil {
		t.Fatal(err)
	}
	if ordered[0].ID != "event-stream" || ordered[1].ID != "capture-agent" {
		t.Fatalf("startup order = %v", []string{ordered[0].ID, ordered[1].ID})
	}
	config.Targets[1].StartAfterHealthy = []string{"capture-agent"}
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("cycle error = %v", err)
	}
}

func validConfigJSON() string {
	return `{
  "schemaVersion":1,
  "checkIntervalMs":1000,
  "targets":[{
    "id":"event-stream",
    "desiredState":"running",
    "startAfterHealthy":[],
    "failureThreshold":2,
    "probes":[
      {"type":"process","executablePath":"C:\\\\Agent\\\\windows-event-stream.exe","requireInteractiveSession":true},
      {"type":"http-json","url":"http://127.0.0.1:8788/healthz","timeoutMs":1000,"expectedStatusCode":200,"expectedJsonStatus":"ok"}
    ],
    "recovery":{
      "scheduledTaskName":"Agent Event Stream",
      "expectedTaskDescription":"owned event stream",
      "maxAttempts":3,
      "attemptWindowMs":300000,
      "backoffMs":1000,
      "actionTimeoutMs":20000,
      "startupGraceMs":5000
    }
  }]
}`
}

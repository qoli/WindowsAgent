package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/actionrun"
)

func TestDirectPointerEndpointAcceptsLiteralInputWithoutRule(t *testing.T) {
	server, _, _ := newTestServerAndRuleRoot(t, &fakeCapturer{status: testStatus(), result: testResult()}, time.Second)
	service := &fakeActionService{directKeyResult: actionrun.Invocation{InvocationID: "act_pointer", State: actionrun.StateRunning}}
	server.actions = service
	body := `{"schemaVersion":1,"operation":"click","expectedForeground":{"processId":42,"executableName":"Fixture.exe","executablePath":"C:\\Apps\\Fixture.exe"},"x":2000,"y":1000,"holdMs":40}`
	for _, extra := range []string{"", `,"ruleId":"invented.exe"`, `,"x":1`} {
		input := strings.TrimSuffix(body, "}") + extra + "}"
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/pointer-inputs/invoke", strings.NewReader(input)))
		if extra == "" {
			if response.Code != http.StatusAccepted || response.Header().Get("Location") != "/v1/action-invocations/act_pointer" || service.directPointer.X == nil || *service.directPointer.X != 2000 {
				t.Fatalf("status=%d body=%s request=%+v", response.Code, response.Body.String(), service.directPointer)
			}
		} else if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid request accepted: status=%d body=%s", response.Code, response.Body.String())
		}
	}
}

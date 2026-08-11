package evidencehttp

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/evidence"
)

const testToken = "0123456789abcdef0123456789abcdef"

func TestRangeRequiresAuthAndEnforcesBound(t *testing.T) {
	store, err := evidence.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(store, testToken, time.Minute, func() Status { return Status{State: "recording"} })
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/evidence/range?from=2026-08-11T12%3A00%3A00Z&to=2026-08-11T12%3A00%3A01Z", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/v1/evidence/range?from=2026-08-11T12%3A00%3A00Z&to=2026-08-11T12%3A02%3A00Z", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d", response.Code)
	}
}

func TestAuthorizedEmptyRangeReturnsZip(t *testing.T) {
	store, err := evidence.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(store, testToken, time.Minute, func() Status { return Status{State: "recording"} })
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/evidence/range?from=2026-08-11T12%3A00%3A00Z&to=2026-08-11T12%3A00%3A01Z", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
}

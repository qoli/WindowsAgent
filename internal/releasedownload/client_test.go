package releasedownload

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/qoli/WindowsAgent/internal/releasecatalog"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestDoRetriesEOFTransportFailure(t *testing.T) {
	attempts := 0
	client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		if attempts < transportAttempts {
			return nil, io.EOF
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			ProtoMajor: 1,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})}}
	response, err := client.do(context.Background(), "https://example.test/release.exe")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if attempts != transportAttempts {
		t.Fatalf("attempts = %d, want %d", attempts, transportAttempts)
	}
}

func TestFetchCatalogForcesHTTP1(t *testing.T) {
	catalog := validCatalog()
	protocol := 0
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protocol = r.ProtoMajor
		var err error
		if strings.HasSuffix(r.URL.Path, "SHA256SUMS") {
			err = releasecatalog.WriteSHA256Sums(w, catalog)
		} else {
			err = releasecatalog.WriteJSON(w, catalog)
		}
		if err != nil {
			t.Fatal(err)
		}
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	client := NewHTTP1Client()
	transport := client.Transport.(*http.Transport)
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // test server only
	loaded, base, err := (Client{HTTP: client}).FetchCatalog(context.Background(), server.URL+"/downloads/windowsagent-release.json")
	if err != nil {
		t.Fatal(err)
	}
	if protocol != 1 {
		t.Fatalf("request protocol major = %d, want 1", protocol)
	}
	if loaded.Version != catalog.Version || base.Path != "/downloads/" {
		t.Fatalf("unexpected catalog/base: %#v %s", loaded, base)
	}
}

func TestStageRejectsInvalidPEWithoutCommitting(t *testing.T) {
	content := []byte("not a PE")
	digest := sha256.Sum256(content)
	catalog := validCatalog()
	catalog.Artifacts[0].Bytes = int64(len(content))
	catalog.Artifacts[0].SHA256 = hex.EncodeToString(digest[:])
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(content) }))
	defer server.Close()
	client := server.Client()
	base, _ := urlParse(server.URL + "/")
	directory := filepath.Join(t.TempDir(), "release")
	err := (Client{HTTP: client}).Stage(context.Background(), base, catalog, directory, func(artifact releasecatalog.Artifact) bool {
		return artifact.Name == catalog.Artifacts[0].Name
	})
	if err == nil || !strings.Contains(err.Error(), "verify downloaded") {
		t.Fatalf("Stage() error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(directory, catalog.Artifacts[0].Name)); !os.IsNotExist(statErr) {
		t.Fatalf("invalid artifact was committed: %v", statErr)
	}
}

func validCatalog() releasecatalog.Catalog {
	artifacts := make([]releasecatalog.Artifact, 0, len(releasecatalog.Specs()))
	for _, spec := range releasecatalog.Specs() {
		artifacts = append(artifacts, releasecatalog.Artifact{Name: spec.Name, Role: spec.Role, Class: spec.Class, Subsystem: spec.Subsystem, Bytes: 1, SHA256: strings.Repeat("a", 64)})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Name < artifacts[j].Name })
	return releasecatalog.Catalog{SchemaVersion: releasecatalog.SchemaVersion, Version: "test", Target: releasecatalog.TargetWindowsAMD64, Artifacts: artifacts}
}

func urlParse(value string) (*url.URL, error) { return url.Parse(value) }

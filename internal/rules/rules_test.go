package rules

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRegistryResolvesExecutableAndReadsAGENTS(t *testing.T) {
	registry := testRegistry(t)
	resolution, err := registry.Resolve("CRIMSONDESERT.EXE")
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Status != StatusMatched || resolution.ID != "CrimsonDesert.exe" {
		t.Fatalf("resolution = %+v", resolution)
	}
	if resolution.Description != MatchedDescription {
		t.Fatalf("description = %q", resolution.Description)
	}
	if resolution.Agents == nil ||
		resolution.Agents.URL != "/v1/rules/CrimsonDesert.exe/AGENTS.md" ||
		len(resolution.Agents.SHA256) != 64 {
		t.Fatalf("AGENTS navigation = %+v", resolution.Agents)
	}
	content, readResolution, err := registry.ReadAGENTS("CrimsonDesert.exe")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# Guidance\n" || readResolution.Agents.SHA256 != resolution.Agents.SHA256 {
		t.Fatal("read AGENTS content or provenance differs from resolution")
	}
	if err := resolution.Validate(); err != nil {
		t.Fatalf("matched resolution rejected: %v", err)
	}
}

func TestRegistryReportsUnmatchedExplicitly(t *testing.T) {
	resolution, err := testRegistry(t).Resolve("explorer.exe")
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Status != StatusUnmatched ||
		resolution.Description != UnmatchedDescription ||
		resolution.ID != "" ||
		resolution.Agents != nil {
		t.Fatalf("resolution = %+v", resolution)
	}
	if err := resolution.Validate(); err != nil {
		t.Fatalf("unmatched resolution rejected: %v", err)
	}
}

func TestResolutionRejectsMissingDescription(t *testing.T) {
	for _, resolution := range []Resolution{
		{Status: StatusUnmatched},
		{
			Status: StatusMatched,
			ID:     "game.exe",
			Agents: &Document{
				URL:         "/v1/rules/game.exe/AGENTS.md",
				ContentType: agentsMediaType,
				SHA256:      strings.Repeat("0", 64),
			},
		},
	} {
		if err := resolution.Validate(); err == nil || !strings.Contains(err.Error(), "description") {
			t.Fatalf("Validate() error = %v, want description error", err)
		}
	}
}

func TestResolveRejectsMissingRegistryOrExecutable(t *testing.T) {
	var registry *Registry
	if _, err := registry.Resolve("game.exe"); err == nil {
		t.Fatal("expected nil registry error")
	}
	if _, err := testRegistry(t).Resolve(""); err == nil {
		t.Fatal("expected missing executable error")
	}
}

func TestRegistryRejectsInvalidDocuments(t *testing.T) {
	tests := []struct {
		name   string
		source fstest.MapFS
		want   string
	}{
		{name: "empty registry", source: fstest.MapFS{}, want: "at least one"},
		{name: "non executable rule", source: fstest.MapFS{"game/AGENTS.md": {Data: []byte("# Guidance\n")}}, want: "must end in .exe"},
		{name: "empty agents", source: fstest.MapFS{"game.exe/AGENTS.md": {Data: []byte(" \n")}}, want: "AGENTS.md is empty"},
		{
			name: "case insensitive duplicate",
			source: fstest.MapFS{
				"game.exe/AGENTS.md": {Data: []byte("# One\n")},
				"GAME.EXE/AGENTS.md": {Data: []byte("# Two\n")},
			},
			want: "duplicate",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(test.source); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestReadAGENTSRejectsUnknownOrNonCanonicalID(t *testing.T) {
	registry := testRegistry(t)
	for _, id := range []string{"unknown.exe", "crimsondesert.exe"} {
		if _, _, err := registry.ReadAGENTS(id); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("ReadAGENTS(%q) error = %v, want fs.ErrNotExist", id, err)
		}
	}
}

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	registry, err := New(fstest.MapFS{
		"CrimsonDesert.exe/AGENTS.md": {Data: []byte("# Guidance\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

package releasecatalog

import (
	"bytes"
	"strings"
	"testing"
)

func TestCatalogValidateAcceptsCompleteCanonicalCatalog(t *testing.T) {
	catalog := testCatalog()
	if err := catalog.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	var encoded bytes.Buffer
	if err := WriteJSON(&encoded, catalog); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	loaded, err := Load(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Version != catalog.Version || len(loaded.Artifacts) != len(catalog.Artifacts) {
		t.Fatalf("Load() = %#v, want version %q and %d artifacts", loaded, catalog.Version, len(catalog.Artifacts))
	}
}

func TestCatalogValidateRejectsDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Catalog)
		want   string
	}{
		{name: "unknown field metadata", mutate: func(c *Catalog) { c.Artifacts[0].Role = "wrong" }, want: "metadata does not match"},
		{name: "uppercase digest", mutate: func(c *Catalog) { c.Artifacts[0].SHA256 = strings.ToUpper(c.Artifacts[0].SHA256) }, want: "64 lowercase"},
		{name: "missing artifact", mutate: func(c *Catalog) { c.Artifacts = c.Artifacts[1:] }, want: "exactly"},
		{name: "unsorted", mutate: func(c *Catalog) { c.Artifacts[0], c.Artifacts[1] = c.Artifacts[1], c.Artifacts[0] }, want: "sorted"},
		{name: "unsafe name", mutate: func(c *Catalog) { c.Artifacts[0].Name = `..\evil.exe` }, want: "safe exe basename"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			catalog := testCatalog()
			test.mutate(&catalog)
			if err := catalog.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestLoadRejectsUnknownAndMultipleJSON(t *testing.T) {
	valid := testCatalog()
	var encoded bytes.Buffer
	if err := WriteJSON(&encoded, valid); err != nil {
		t.Fatal(err)
	}
	unknown := strings.Replace(encoded.String(), `"target": "windows-amd64",`, `"target": "windows-amd64", "unknown": true,`, 1)
	if _, err := Load(strings.NewReader(unknown)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Load(unknown) error = %v", err)
	}
	if _, err := Load(strings.NewReader(encoded.String() + `{}`)); err == nil || !strings.Contains(err.Error(), "multiple JSON") {
		t.Fatalf("Load(multiple) error = %v", err)
	}
}

func TestWriteSHA256SumsUsesCatalogOrder(t *testing.T) {
	catalog := testCatalog()
	var output bytes.Buffer
	if err := WriteSHA256Sums(&output, catalog); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != len(catalog.Artifacts) {
		t.Fatalf("got %d lines, want %d", len(lines), len(catalog.Artifacts))
	}
	for index, artifact := range catalog.Artifacts {
		want := artifact.SHA256 + "  " + artifact.Name
		if lines[index] != want {
			t.Fatalf("line %d = %q, want %q", index, lines[index], want)
		}
	}
	if err := ValidateSHA256Sums(strings.NewReader(output.String()), catalog); err != nil {
		t.Fatalf("ValidateSHA256Sums() error = %v", err)
	}
	broken := strings.Replace(output.String(), strings.Repeat("a", 64), strings.Repeat("b", 64), 1)
	if err := ValidateSHA256Sums(strings.NewReader(broken), catalog); err == nil || !strings.Contains(err.Error(), "disagrees") {
		t.Fatalf("ValidateSHA256Sums(broken) error = %v", err)
	}
}

func testCatalog() Catalog {
	specs := Specs()
	artifacts := make([]Artifact, 0, len(specs))
	for _, spec := range specs {
		artifacts = append(artifacts, Artifact{
			Name: spec.Name, Role: spec.Role, Class: spec.Class, Subsystem: spec.Subsystem,
			Bytes: 123, SHA256: strings.Repeat("a", 64),
		})
	}
	for i := 0; i < len(artifacts); i++ {
		for j := i + 1; j < len(artifacts); j++ {
			if artifacts[j].Name < artifacts[i].Name {
				artifacts[i], artifacts[j] = artifacts[j], artifacts[i]
			}
		}
	}
	return Catalog{SchemaVersion: SchemaVersion, Version: "0.1.0-test", Target: TargetWindowsAMD64, Artifacts: artifacts}
}

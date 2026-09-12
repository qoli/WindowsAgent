package windowsautomation

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageLoadsDirectoryAndDeterministicArchive(t *testing.T) {
	root := writePackage(t, `def main(ctx):
    while False:
        pass
    task.elapsed_milliseconds()
    return {"value": ctx.inputs["value"]}
`, `{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`, `{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`, 1<<20)
	pkg, err := LoadDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.Version != 1 || len(pkg.Digest) != 64 {
		t.Fatalf("package = %+v", pkg)
	}
	archiveOne, err := ArchiveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	archiveTwo, err := ArchiveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(archiveOne, archiveTwo) {
		t.Fatal("ArchiveDirectory is not deterministic")
	}
	fromArchive, err := LoadArchive(archiveOne)
	if err != nil {
		t.Fatal(err)
	}
	if fromArchive.Digest != pkg.Digest {
		t.Fatalf("archive digest = %s, directory digest = %s", fromArchive.Digest, pkg.Digest)
	}
	if err := pkg.ValidateInputs(map[string]any{"value": 3}); err == nil {
		t.Fatal("invalid input unexpectedly passed schema")
	}
}

func TestPackageRejectsLoadUndeclaredAndMalformedManifest(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(string)
		want   string
	}{
		{"load", func(root string) {
			writeTestFile(t, filepath.Join(root, "main.star"), "load(\"other.star\", \"x\")\ndef main(ctx): return {}\n")
		}, "load"},
		{"undeclared", func(root string) { writeTestFile(t, filepath.Join(root, "extra.txt"), "extra") }, "members do not match"},
		{"unknown manifest field", func(root string) {
			name := filepath.Join(root, ManifestName)
			data, _ := os.ReadFile(name)
			writeTestFile(t, name, strings.Replace(string(data), `"schemaVersion":1`, `"schemaVersion":1,"unknown":true`, 1))
		}, "unknown field"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writePackage(t, `def main(ctx): return {}`, `{}`, `{}`, 100)
			test.mutate(root)
			_, err := LoadDirectory(root)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.want)) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestPackageRequiresExactMainContextEntrypoint(t *testing.T) {
	for _, script := range []string{
		`def other(ctx): return {}`,
		`def main(): return {}`,
		`def main(input): return {}`,
	} {
		root := writePackage(t, script, `{}`, `{}`, 100)
		if _, err := LoadDirectory(root); err == nil || !strings.Contains(err.Error(), "main") {
			t.Fatalf("script %q error = %v", script, err)
		}
	}
}

func TestLoadArchiveRejectsNonCanonicalOrderAndMember(t *testing.T) {
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, name := range []string{"z.txt", "a.txt"} {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetMode(0o600)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadArchive(output.Bytes()); err == nil || !strings.Contains(err.Error(), "lexically ordered") {
		t.Fatalf("error = %v", err)
	}
}

func writePackage(t *testing.T, script, inputSchema, outputSchema string, maxResult uint64) string {
	t.Helper()
	root := t.TempDir()
	manifest := `{
  "schemaVersion":1,
  "version":1,
  "title":"Fixture automation",
  "entrypoint":"main.star",
  "taskDocument":"TASK.md",
  "inputSchema":"input.schema.json",
  "outputSchema":"output.schema.json",
  "files":["main.star","TASK.md","input.schema.json","output.schema.json"],
  "limits":{"wallTimeMs":5000,"maxSteps":100000,"maxResultBytes":` + uintString(maxResult) + `,"maxProcessOutputBytes":1048576}
}`
	for name, content := range map[string]string{
		ManifestName: manifest, "main.star": script, "TASK.md": "# Fixture\n",
		"input.schema.json": inputSchema, "output.schema.json": outputSchema,
	} {
		writeTestFile(t, filepath.Join(root, name), content)
	}
	return root
}

func writeTestFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func uintString(value uint64) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunReportsValidPackageAndRejectsInvalidSource(t *testing.T) {
	root := writeCheckPackage(t, "def main(ctx):\n    return {}\n")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--package", root, "--json"}, &stdout, &stderr); code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), `"valid": true`) {
		t.Fatalf("valid code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if err := os.WriteFile(filepath.Join(root, "main.star"), []byte("def main(:\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--package", root, "--json"}, &stdout, &stderr); code != 1 || !strings.Contains(stdout.String(), `"valid": false`) {
		t.Fatalf("invalid code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func writeCheckPackage(t *testing.T, source string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"manifest.json":      `{"schemaVersion":1,"version":1,"title":"Check fixture","entrypoint":"main.star","taskDocument":"TASK.md","inputSchema":"input.schema.json","outputSchema":"output.schema.json","files":["main.star","TASK.md","input.schema.json","output.schema.json"],"limits":{"wallTimeMs":1000,"maxSteps":1000,"maxResultBytes":1024,"maxProcessOutputBytes":1024}}`,
		"main.star":          source,
		"TASK.md":            "# Check fixture\n",
		"input.schema.json":  `{}`,
		"output.schema.json": `{}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

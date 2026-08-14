package puredecision

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qoli/WindowsAgent/internal/scriptpackage"
)

func TestRunExecutesPermissionFreePackage(t *testing.T) {
	pkg := writePackage(t, `def main(ctx):
    return {"accepted": ctx.inputs["value"] == 7}
`, `{"type":"object","additionalProperties":false,"required":["value"],"properties":{"value":{"type":"integer"}}}`, `{"type":"object","additionalProperties":false,"required":["accepted"],"properties":{"accepted":{"type":"boolean"}}}`, false)
	output, err := Run(context.Background(), pkg, map[string]any{"value": 7})
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != `{"accepted":true}` {
		t.Fatalf("output = %s", output)
	}
}

func TestRunRejectsObserverPermission(t *testing.T) {
	pkg := writePackage(t, "def main(ctx):\n    return {}\n", `{"type":"object"}`, `{"type":"object"}`, true)
	_, err := Run(context.Background(), pkg, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "must not declare permissions") {
		t.Fatalf("error = %v", err)
	}
}

func writePackage(t *testing.T, script, inputSchema, outputSchema string, screen bool) *scriptpackage.Package {
	t.Helper()
	root := t.TempDir()
	permissions := `{"memory":null,"file":null,"screen":null}`
	if screen {
		permissions = `{"memory":null,"file":null,"screen":{"operations":["readRegion"],"maxCalls":1,"maxPixels":1}}`
	}
	files := map[string]string{
		"main.star": script, "TASK.md": "# Fixture\n", "input.schema.json": inputSchema, "output.schema.json": outputSchema,
		"manifest.json": `{"schemaVersion":2,"version":1,"title":"Fixture","entrypoint":"main.star","taskDocument":"TASK.md","inputSchema":"input.schema.json","outputSchema":"output.schema.json","files":["main.star","TASK.md","input.schema.json","output.schema.json"],"permissions":` + permissions + `,"limits":{"wallTimeMs":1000,"maxSteps":10000,"maxResultBytes":4096,"maxLogBytes":1024}}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := scriptpackage.Load(root, "fixture/decision")
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

package ocraction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAcceptsArbitraryAspectDigitConstraint(t *testing.T) {
	root := writeFixture(t, `{
  "schemaVersion":2,
  "title":"Read digits",
  "inputSchema":"input.schema.json",
  "outputSchema":"output.schema.json",
  "referenceRegion":{"x":1100,"y":815,"w":65,"h":50},
  "sampling":"reference",
  "maxPixels":3250,
  "characterConstraint":"digits"
}`)
	config, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if config.ReferenceRegion.Width != 65 || config.ReferenceRegion.Height != 50 || config.CharacterConstraint != "digits" {
		t.Fatalf("config = %#v", config)
	}
}

func TestLoadRejectsMissingCharacterConstraint(t *testing.T) {
	root := writeFixture(t, `{
  "schemaVersion":2,
  "title":"Read digits",
  "inputSchema":"input.schema.json",
  "outputSchema":"output.schema.json",
  "referenceRegion":{"x":1100,"y":815,"w":65,"h":50},
  "sampling":"reference",
  "maxPixels":3250
}`)
	_, err := Load(root)
	if err == nil || !strings.Contains(err.Error(), "characterConstraint") {
		t.Fatalf("error = %v", err)
	}
}

func writeFixture(t *testing.T, manifest string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range map[string]string{
		"manifest.json":      manifest,
		"input.schema.json":  `{}`,
		"output.schema.json": `{}`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

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

func TestLoadAndApplyRGBThresholdPixelFilter(t *testing.T) {
	root := writeFixture(t, `{
  "schemaVersion":2,
  "title":"Read filtered digits",
  "inputSchema":"input.schema.json",
  "outputSchema":"output.schema.json",
  "referenceRegion":{"x":810,"y":800,"w":55,"h":50},
  "sampling":"reference",
  "maxPixels":2750,
  "characterConstraint":"digits",
  "pixelFilter":{"mode":"rgb-threshold","minRed":120,"minGreen":30,"maxBlue":200,"minRedMinusBlue":25,"minGreenMinusBlue":10}
}`)
	config, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	rgb := []byte{255, 120, 10, 255, 255, 255, 20, 200, 200}
	filtered, err := config.PixelFilter.Apply(rgb)
	if err != nil {
		t.Fatal(err)
	}
	if filtered != 2 || rgb[0] != 255 || rgb[1] != 120 || rgb[2] != 10 || rgb[3] != 0 || rgb[6] != 0 {
		t.Fatalf("filtered=%d rgb=%v", filtered, rgb)
	}
}

func TestLoadRejectsUnknownPixelFilterMode(t *testing.T) {
	root := writeFixture(t, `{
  "schemaVersion":2,
  "title":"Read filtered digits",
  "inputSchema":"input.schema.json",
  "outputSchema":"output.schema.json",
  "referenceRegion":{"x":810,"y":800,"w":55,"h":50},
  "sampling":"reference",
  "maxPixels":2750,
  "characterConstraint":"digits",
  "pixelFilter":{"mode":"hud-orange","minRed":120,"minGreen":30,"maxBlue":200,"minRedMinusBlue":25,"minGreenMinusBlue":10}
}`)
	_, err := Load(root)
	if err == nil || !strings.Contains(err.Error(), "rgb-threshold") {
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

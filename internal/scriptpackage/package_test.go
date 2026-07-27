package scriptpackage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCrimsonInventoryPackage(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "ObservationScripts", "CrimsonDesert", "inventory"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if pkg.Identity.ID != "crimson-desert/inventory" || pkg.Identity.PackageSHA256 == "" {
		t.Fatalf("unexpected identity: %#v", pkg.Identity)
	}
}

func TestLoadRejectsDigestMismatch(t *testing.T) {
	source, _ := filepath.Abs(filepath.Join("..", "..", "ObservationScripts", "CrimsonDesert", "inventory"))
	root := t.TempDir()
	for _, name := range []string{"manifest.json", "TASK.md", "main.star", "output.schema.json"} {
		content, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "main.star"), []byte("def main(ctx): return {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("Load succeeded after script digest mismatch")
	}
}

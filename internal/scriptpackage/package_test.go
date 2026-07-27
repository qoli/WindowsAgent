package scriptpackage

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func inventoryPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "CrimsonDesert.exe", "Scripts", "inventory"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func copyInventoryPackage(t *testing.T) string {
	t.Helper()
	source := inventoryPackageRoot(t)
	root := t.TempDir()
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == "." {
			return err
		}
		target := filepath.Join(root, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o600)
	}); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLoadCrimsonInventoryPackage(t *testing.T) {
	root := inventoryPackageRoot(t)
	pkg, err := Load(root, "crimson-desert/inventory")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if pkg.Identity.ID != "crimson-desert/inventory" || pkg.Identity.Version != 4 {
		t.Fatalf("unexpected identity: %#v", pkg.Identity)
	}
	validInputs := map[string]any{}
	if err := pkg.ValidateInputs(validInputs); err != nil {
		t.Fatalf("ValidateInputs: %v", err)
	}
	for _, invalid := range []map[string]any{
		{"unknown": true},
	} {
		if err := pkg.ValidateInputs(invalid); err == nil {
			t.Fatalf("ValidateInputs accepted %#v", invalid)
		}
	}
}

func TestNestedPackageArtifactUsesPlatformIndependentSlashPath(t *testing.T) {
	if err := validateRelativeName("native/windows-amd64/fixture.dll"); err != nil {
		t.Fatalf("canonical nested package path was rejected: %v", err)
	}
	for _, invalid := range []string{
		`native\windows-amd64\fixture.dll`,
		`C:\native\fixture.dll`,
		"native/../fixture.dll",
	} {
		if err := validateRelativeName(invalid); err == nil {
			t.Fatalf("invalid package path %q was accepted", invalid)
		}
	}
}

func TestLoadRejectsUndeclaredNativeLibraryPathField(t *testing.T) {
	root := copyInventoryPackage(t)
	manifestBytes, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	libraries := manifest["nativeLibraries"].(map[string]any)
	dependency := libraries["save-decoder"].(map[string]any)
	dependency["absolutePath"] = `C:\untrusted\decoder.dll`
	manifestBytes, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, "crimson-desert/inventory"); err == nil {
		t.Fatal("Load accepted an undeclared native library path field")
	}
}

func TestLoadRejectsMissingNativeLibraryArtifact(t *testing.T) {
	root := copyInventoryPackage(t)
	path := filepath.Join(root, "native", "windows-amd64", "crimson-rs.inventory.bb730180.dll")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, "crimson-desert/inventory"); err == nil {
		t.Fatal("Load accepted a missing native library artifact")
	}
}

func TestLoadAcceptsLocallyModifiedDeclaredNativeLibrary(t *testing.T) {
	root := copyInventoryPackage(t)
	path := filepath.Join(root, "native", "windows-amd64", "crimson-rs.inventory.bb730180.dll")
	if err := os.WriteFile(path, []byte("not the declared DLL"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, "crimson-desert/inventory"); err != nil {
		t.Fatalf("Load rejected a locally modified declared DLL: %v", err)
	}
}

func TestLoadAcceptsLocallyModifiedDeclaredScript(t *testing.T) {
	root := copyInventoryPackage(t)
	if err := os.WriteFile(
		filepath.Join(root, "main.star"),
		[]byte("def main(ctx):\n    return {\"schemaVersion\": 1, \"source\": {\"kind\": \"save-file\", \"saveModifiedAt\": None}, \"attempts\": [], \"inventory\": {\"recordCount\": 0, \"occupiedCount\": 0, \"items\": []}}\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, "crimson-desert/inventory"); err != nil {
		t.Fatalf("Load rejected a locally modified declared script: %v", err)
	}
}

func TestLoadRejectsManifestV1(t *testing.T) {
	root := copyInventoryPackage(t)
	manifestBytes, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["schemaVersion"] = float64(1)
	manifestBytes, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, "crimson-desert/inventory"); err == nil {
		t.Fatal("Load accepted manifest schema V1")
	}
}

func TestLoadRejectsCallerBoundFileRootManifestShape(t *testing.T) {
	root := copyInventoryPackage(t)
	manifestBytes, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	permissions := manifest["permissions"].(map[string]any)
	file := permissions["file"].(map[string]any)
	file["roots"] = []any{"crimson-desert-saves"}
	manifestBytes, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, "crimson-desert/inventory"); err == nil {
		t.Fatal("Load accepted legacy caller-bound file root declarations")
	}
}

func TestLoadRejectsMissingInputSchema(t *testing.T) {
	root := copyInventoryPackage(t)
	if err := os.Remove(filepath.Join(root, "input.schema.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, "crimson-desert/inventory"); err == nil {
		t.Fatal("Load accepted a missing input schema")
	}
}

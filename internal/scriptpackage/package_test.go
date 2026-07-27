package scriptpackage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func inventoryPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "ObservationScripts", "CrimsonDesert", "inventory"))
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
	pkg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if pkg.Identity.ID != "crimson-desert/inventory" || pkg.Identity.PackageSHA256 == "" {
		t.Fatalf("unexpected identity: %#v", pkg.Identity)
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
	if _, err := Load(root); err == nil {
		t.Fatal("Load accepted an undeclared native library path field")
	}
}

func TestLoadRejectsMissingNativeLibraryArtifact(t *testing.T) {
	root := copyInventoryPackage(t)
	path := filepath.Join(root, "native", "windows-amd64", "crimson-rs.inventory.bb730180.dll")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("Load accepted a missing native library artifact")
	}
}

func TestLoadRejectsNativeLibraryDigestMismatch(t *testing.T) {
	root := copyInventoryPackage(t)
	path := filepath.Join(root, "native", "windows-amd64", "crimson-rs.inventory.bb730180.dll")
	if err := os.WriteFile(path, []byte("not the declared DLL"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("Load accepted a native library digest mismatch")
	}
}

func TestNativeLibraryArtifactChangesPackageDigest(t *testing.T) {
	original, err := Load(inventoryPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	root := copyInventoryPackage(t)
	artifact := "native/windows-amd64/crimson-rs.inventory.bb730180.dll"
	path := filepath.Join(root, filepath.FromSlash(artifact))
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = append(content, 0)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	manifestBytes, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["files"].(map[string]any)[artifact].(map[string]any)["sha256"] = digest
	manifest["nativeLibraries"].(map[string]any)["save-decoder"].(map[string]any)["sha256"] = digest
	manifestBytes, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Identity.PackageSHA256 == original.Identity.PackageSHA256 {
		t.Fatal("native library artifact did not affect package digest")
	}
}

func TestLoadRejectsDigestMismatch(t *testing.T) {
	root := copyInventoryPackage(t)
	if err := os.WriteFile(filepath.Join(root, "main.star"), []byte("def main(ctx): return {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("Load succeeded after script digest mismatch")
	}
}

package assistbackend

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestElevatedHelperRequiresBothProcessIDs(t *testing.T) {
	for _, args := range [][]string{
		{"--assist-apply", "install", "stage", "catalog", "data", "true", "42"},
		{"--assist-uninstall", "data", "42"},
	} {
		handled, err := RunElevatedHelper(context.Background(), args)
		if !handled || err == nil {
			t.Fatalf("args=%v handled=%v error=%v", args, handled, err)
		}
	}
}

func TestPrepareApplyHandoffStagesRunningBootstrap(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	wantHelper, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"install", "update", "repair"} {
		t.Run(operation, func(t *testing.T) {
			stage := t.TempDir()
			catalogPath := filepath.Join(stage, "windowsagent-release.json")
			catalog := []byte("catalog remains the published release metadata")
			if err := os.WriteFile(catalogPath, catalog, 0600); err != nil {
				t.Fatal(err)
			}
			helper, args, err := prepareApplyHandoff(operation, stage, catalogPath, "data", true, 42, 43)
			if err != nil {
				t.Fatal(err)
			}
			if helper != filepath.Join(stage, "windows-assist-backend.exe") {
				t.Fatalf("helper = %q", helper)
			}
			got, err := os.ReadFile(helper)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, wantHelper) {
				t.Fatal("handoff did not preserve the running backend bytes")
			}
			wantArgs := []string{"--assist-apply", operation, stage, catalogPath, "data", "true", "42", "43"}
			if !reflect.DeepEqual(args, wantArgs) {
				t.Fatalf("args = %v", args)
			}
			gotCatalog, err := os.ReadFile(catalogPath)
			if err != nil || !bytes.Equal(gotCatalog, catalog) {
				t.Fatalf("release metadata changed: %v", err)
			}
		})
	}
}

func TestPrepareApplyHandoffDoesNotLaunchWithoutStagedHelper(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		stage := filepath.Join(t.TempDir(), "stage")
		if blocked {
			if err := os.MkdirAll(filepath.Join(stage, "windows-assist-backend.exe"), 0700); err != nil {
				t.Fatal(err)
			}
		}
		helper, args, err := prepareApplyHandoff("install", stage, filepath.Join(stage, "windowsagent-release.json"), "data", false, 42, 43)
		if err == nil || helper != "" || args != nil {
			t.Fatalf("helper=%q args=%v error=%v", helper, args, err)
		}
	}
}

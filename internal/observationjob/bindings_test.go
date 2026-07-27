package observationjob

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/qoli/WindowsAgent/internal/observer"
	"github.com/qoli/WindowsAgent/internal/scriptpackage"
)

func inventoryPackage(t *testing.T) *scriptpackage.Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join(
		"..",
		"..",
		"Rules",
		"CrimsonDesert.exe",
		"Scripts",
		"inventory",
	))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := scriptpackage.Load(root, "crimson-desert/inventory")
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestValidateBindingsUsesPackageOwnedInputs(t *testing.T) {
	spec := Spec{
		Process: &observer.ProcessIdentity{PID: 42},
		Inputs:  map[string]any{},
	}
	if err := validateBindings(inventoryPackage(t), spec); err != nil {
		t.Fatalf("validateBindings: %v", err)
	}
}

func TestValidateBindingsRejectsMissingProcessOrCallerInputs(t *testing.T) {
	tests := []struct {
		name string
		spec Spec
		want string
	}{
		{
			name: "missing process",
			spec: Spec{
				Inputs: map[string]any{},
			},
			want: "process",
		},
		{
			name: "caller supplied inventory state",
			spec: Spec{
				Process: &observer.ProcessIdentity{PID: 42},
				Inputs:  map[string]any{"save": "caller-owned"},
			},
			want: "input schema",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateBindings(inventoryPackage(t), test.spec); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateBindings() error = %v, want %q", err, test.want)
			}
		})
	}
}

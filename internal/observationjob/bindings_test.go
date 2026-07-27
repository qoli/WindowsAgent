package observationjob

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/qoli/WindowsAgent/internal/observer"
	"github.com/qoli/WindowsAgent/internal/scriptpackage"
)

func TestValidateBindingsUsesPackageContract(t *testing.T) {
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
	spec := Spec{
		Process:   &observer.ProcessIdentity{PID: 42},
		FileRoots: map[string]string{"crimson-desert-saves": t.TempDir()},
		Inputs: map[string]any{
			"save": map[string]any{
				"root":     "crimson-desert-saves",
				"relative": "slot1/save.save",
			},
		},
	}
	if err := validateBindings(pkg, spec); err != nil {
		t.Fatalf("validateBindings: %v", err)
	}
}

func TestValidateBindingsRejectsMissingOrUndeclaredState(t *testing.T) {
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
	validInputs := map[string]any{
		"save": map[string]any{
			"root":     "crimson-desert-saves",
			"relative": "slot1/save.save",
		},
	}
	tests := []struct {
		name string
		spec Spec
		want string
	}{
		{
			name: "missing process",
			spec: Spec{
				FileRoots: map[string]string{"crimson-desert-saves": t.TempDir()},
				Inputs:    validInputs,
			},
			want: "process",
		},
		{
			name: "missing root",
			spec: Spec{
				Process:   &observer.ProcessIdentity{PID: 42},
				FileRoots: map[string]string{},
				Inputs:    validInputs,
			},
			want: "binding count",
		},
		{
			name: "extra root",
			spec: Spec{
				Process: &observer.ProcessIdentity{PID: 42},
				FileRoots: map[string]string{
					"crimson-desert-saves": t.TempDir(),
					"other":                t.TempDir(),
				},
				Inputs: validInputs,
			},
			want: "binding count",
		},
		{
			name: "invalid inputs",
			spec: Spec{
				Process:   &observer.ProcessIdentity{PID: 42},
				FileRoots: map[string]string{"crimson-desert-saves": t.TempDir()},
				Inputs:    map[string]any{},
			},
			want: "input schema",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateBindings(pkg, test.spec); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateBindings() error = %v, want %q", err, test.want)
			}
		})
	}
}

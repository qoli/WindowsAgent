package observationjob

import (
	"testing"

	"github.com/qoli/WindowsAgent/internal/scriptpackage"
)

func TestRequiresObserverOnlyForDeclaredPermissionNamespaces(t *testing.T) {
	if requiresObserver(scriptpackage.Permissions{}) {
		t.Fatal("pure package unexpectedly requires observer process")
	}
	for _, permissions := range []scriptpackage.Permissions{
		{Memory: &scriptpackage.MemoryPermissions{}},
		{File: &scriptpackage.FilePermissions{}},
		{Screen: &scriptpackage.ScreenPermissions{}},
	} {
		if !requiresObserver(permissions) {
			t.Fatalf("permissions %+v do not require observer process", permissions)
		}
	}
}

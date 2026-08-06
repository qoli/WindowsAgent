package observationapi

import "testing"

func TestFileDecodeIsNotAnObserverOperation(t *testing.T) {
	if OperationAllowed(NamespaceFile, "decode") {
		t.Fatal("legacy file.decode remains available")
	}
	if !OperationAllowed(NamespaceFile, "openBlob") {
		t.Fatal("generic file.openBlob is unavailable")
	}
	if !OperationAllowed(NamespaceFile, "list") {
		t.Fatal("generic bounded file.list is unavailable")
	}
}

func TestScreenSurfaceIsOnlyBoundedRegionRead(t *testing.T) {
	if !OperationAllowed(NamespaceScreen, "readRegion") {
		t.Fatal("generic screen.readRegion is unavailable")
	}
	for _, operation := range []string{"capture", "findCompass", "readScreen"} {
		if OperationAllowed(NamespaceScreen, operation) {
			t.Fatalf("game-specific or unbounded screen operation %q is available", operation)
		}
	}
}

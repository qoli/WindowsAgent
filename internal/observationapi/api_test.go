package observationapi

import "testing"

func TestFileDecodeIsNotAnObserverOperation(t *testing.T) {
	if OperationAllowed(NamespaceFile, "decode") {
		t.Fatal("legacy file.decode remains available")
	}
	if !OperationAllowed(NamespaceFile, "openBlob") {
		t.Fatal("generic file.openBlob is unavailable")
	}
}

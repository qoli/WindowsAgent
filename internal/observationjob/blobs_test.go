package observationjob

import "testing"

func TestBlobHandleNotIssuedByJobIsRejected(t *testing.T) {
	catalog := newBlobCatalog(t.TempDir())
	_, err := catalog.path(map[string]any{
		"blobHandle": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err == nil {
		t.Fatal("forged blob handle was accepted")
	}
}

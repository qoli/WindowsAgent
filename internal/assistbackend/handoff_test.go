package assistbackend

import (
	"context"
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

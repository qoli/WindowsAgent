package pixels

import (
	"bytes"
	"testing"
)

func TestRGB24WordsToBGRXBottomUp(t *testing.T) {
	got, err := RGB24WordsToBGRXBottomUp([]uint32{0x0a141e, 0x28323c}, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x3c, 0x32, 0x28, 0xff, 0x1e, 0x14, 0x0a, 0xff}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

package observer

import (
	"context"
	"reflect"
	"testing"
)

func TestPatternWildcardScan(t *testing.T) {
	pattern, err := parsePattern("48 8B ?? 01")
	if err != nil {
		t.Fatal(err)
	}
	got := scanPattern([]byte{0, 0x48, 0x8b, 0x44, 1, 0x48, 0x8b, 0x99, 1}, pattern, 10)
	if !reflect.DeepEqual(got, []int{1, 5}) {
		t.Fatalf("matches = %#v", got)
	}
}

func TestPatternScanUsesKnownAnchorWithWildcards(t *testing.T) {
	pattern, err := parsePattern("?? 89 ?? ?? ?? ?? ?? ?? 8D ?? 30 01 00 00")
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 1<<20)
	copy(data[700000:], []byte{0x44, 0x89, 1, 2, 3, 4, 5, 6, 0x8d, 9, 0x30, 0x01, 0, 0})
	got, err := scanPatternContext(context.Background(), data, pattern, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int{700000}) {
		t.Fatalf("matches = %#v", got)
	}
}

func TestPatternScanHonorsCancelledContext(t *testing.T) {
	pattern, err := parsePattern("48 8B ?? 01")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := scanPatternContext(ctx, make([]byte, 1<<20), pattern, 1); err == nil {
		t.Fatal("cancelled scan succeeded")
	}
}

func TestDecodeTypedRecordFields(t *testing.T) {
	data := []byte{0x34, 0x12, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0}
	id, err := decodeTyped(data[:2], "u16")
	if err != nil || id != uint64(0x1234) {
		t.Fatalf("id = %#v, err = %v", id, err)
	}
	quantity, err := decodeTyped(data[4:12], "u64")
	if err != nil || quantity != uint64(3) {
		t.Fatalf("quantity = %#v, err = %v", quantity, err)
	}
}

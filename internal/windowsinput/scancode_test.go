package windowsinput

import "testing"

func TestDecodeMappedScanCode(t *testing.T) {
	tests := []struct {
		name     string
		mapped   uintptr
		scanCode uint16
		extended bool
	}{
		{name: "ordinary", mapped: 0x1f, scanCode: 0x1f},
		{name: "e0 extended", mapped: 0xe04d, scanCode: 0x4d, extended: true},
		{name: "e1 extended", mapped: 0xe11d, scanCode: 0x1d, extended: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scanCode, extended, err := decodeMappedScanCode(test.mapped)
			if err != nil {
				t.Fatalf("decodeMappedScanCode: %v", err)
			}
			if scanCode != test.scanCode || extended != test.extended {
				t.Fatalf("got scanCode=%#x extended=%v, want scanCode=%#x extended=%v", scanCode, extended, test.scanCode, test.extended)
			}
		})
	}
}

func TestDecodeMappedScanCodeRejectsInvalidValues(t *testing.T) {
	for _, mapped := range []uintptr{0, 0x1200, 0x121f} {
		if _, _, err := decodeMappedScanCode(mapped); err == nil {
			t.Fatalf("decodeMappedScanCode(%#x) unexpectedly succeeded", mapped)
		}
	}
}

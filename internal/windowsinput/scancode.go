package windowsinput

import "fmt"

func decodeMappedScanCode(mapped uintptr) (uint16, bool, error) {
	if mapped == 0 {
		return 0, false, fmt.Errorf("MapVirtualKeyW returned no scan code")
	}

	prefix := uint16((mapped >> 8) & 0xff)
	scanCode := uint16(mapped & 0xff)
	if scanCode == 0 {
		return 0, false, fmt.Errorf("MapVirtualKeyW returned an invalid scan code %#x", mapped)
	}
	switch prefix {
	case 0:
		return scanCode, false, nil
	case 0xe0, 0xe1:
		return scanCode, true, nil
	default:
		return 0, false, fmt.Errorf("MapVirtualKeyW returned unsupported scan-code prefix %#x", prefix)
	}
}

package windowsinput

import "testing"

func TestVirtualKeySupportsCanonicalKeyboardSet(t *testing.T) {
	for key, want := range map[string]uint16{
		"Key_A": 0x41, "Key_Z": 0x5A, "Key_0": 0x30, "Key_9": 0x39,
		"Key_F1": 0x70, "Key_F24": 0x87, "Key_Space": 0x20, "Key_Minus": 0xBD,
		"Key_RightControl": 0xA3,
		"Key_Left":         0x25, "Key_Up": 0x26, "Key_Right": 0x27, "Key_Down": 0x28,
		"Key_LeftArrow": 0x25, "Key_UpArrow": 0x26, "Key_RightArrow": 0x27, "Key_DownArrow": 0x28,
	} {
		got, err := VirtualKey(key)
		if err != nil || got != want {
			t.Fatalf("VirtualKey(%q) = 0x%X, %v; want 0x%X", key, got, err, want)
		}
	}
}

func TestVirtualKeyRejectsNonCanonicalOrUnsupportedKeys(t *testing.T) {
	for _, key := range []string{"A", "Key_a", "Key_F0", "Key_F25", "Key_PrintScreen", ""} {
		if _, err := VirtualKey(key); err == nil {
			t.Fatalf("VirtualKey(%q) unexpectedly succeeded", key)
		}
	}
}

func TestRequiresExtendedScanCodeForNavigationAndRightModifiers(t *testing.T) {
	for _, key := range []string{
		"Key_PageUp", "Key_PageDown", "Key_End", "Key_Home", "Key_Insert", "Key_Delete",
		"Key_Left", "Key_Up", "Key_Right", "Key_Down",
		"Key_LeftArrow", "Key_UpArrow", "Key_RightArrow", "Key_DownArrow",
		"Key_RightControl", "Key_RightAlt",
	} {
		if !RequiresExtendedScanCode(key) {
			t.Fatalf("RequiresExtendedScanCode(%q)=false", key)
		}
	}
	for _, key := range []string{"Key_A", "Key_Space", "Key_LeftControl", "Key_LeftAlt", "Key_LeftShift", "Key_RightShift"} {
		if RequiresExtendedScanCode(key) {
			t.Fatalf("RequiresExtendedScanCode(%q)=true", key)
		}
	}
}

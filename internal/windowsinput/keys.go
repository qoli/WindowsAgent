package windowsinput

import (
	"fmt"
	"strconv"
	"strings"
)

func VirtualKey(key string) (uint16, error) {
	if len(key) == len("Key_A") && strings.HasPrefix(key, "Key_") {
		letter := key[len(key)-1]
		if letter >= 'A' && letter <= 'Z' {
			return uint16(letter), nil
		}
		if letter >= '0' && letter <= '9' {
			return uint16(letter), nil
		}
	}
	if strings.HasPrefix(key, "Key_F") {
		number, err := strconv.Atoi(strings.TrimPrefix(key, "Key_F"))
		if err == nil && number >= 1 && number <= 24 {
			return uint16(0x70 + number - 1), nil
		}
	}
	keys := map[string]uint16{
		"Key_Backspace":    0x08,
		"Key_Tab":          0x09,
		"Key_Enter":        0x0D,
		"Key_LeftShift":    0xA0,
		"Key_RightShift":   0xA1,
		"Key_LeftControl":  0xA2,
		"Key_RightControl": 0xA3,
		"Key_LeftAlt":      0xA4,
		"Key_RightAlt":     0xA5,
		"Key_Escape":       0x1B,
		"Key_Space":        0x20,
		"Key_PageUp":       0x21,
		"Key_PageDown":     0x22,
		"Key_End":          0x23,
		"Key_Home":         0x24,
		"Key_Left":         0x25,
		"Key_LeftArrow":    0x25,
		"Key_Up":           0x26,
		"Key_UpArrow":      0x26,
		"Key_Right":        0x27,
		"Key_RightArrow":   0x27,
		"Key_Down":         0x28,
		"Key_DownArrow":    0x28,
		"Key_Insert":       0x2D,
		"Key_Delete":       0x2E,
	}
	if virtualKey, ok := keys[key]; ok {
		return virtualKey, nil
	}
	return 0, fmt.Errorf("unsupported Windows input key %q", key)
}

func RequiresExtendedScanCode(key string) bool {
	switch key {
	case "Key_PageUp", "Key_PageDown", "Key_End", "Key_Home",
		"Key_Left", "Key_LeftArrow", "Key_Up", "Key_UpArrow",
		"Key_Right", "Key_RightArrow", "Key_Down", "Key_DownArrow",
		"Key_Insert", "Key_Delete", "Key_RightControl", "Key_RightAlt":
		return true
	default:
		return false
	}
}

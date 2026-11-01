package watchdog

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"testing"
)

func liveCheckGUIBinary(binary []byte) error {
	image, err := pe.NewFile(bytes.NewReader(binary))
	if err != nil {
		return fmt.Errorf("read acceptance PE image: %w", err)
	}
	defer image.Close()
	header, ok := image.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || header.Subsystem != pe.IMAGE_SUBSYSTEM_WINDOWS_GUI {
		return fmt.Errorf("acceptance runner and Task helpers require a 64-bit GUI PE image; rebuild go test -c with -ldflags=-H=windowsgui")
	}
	return nil
}

func TestWatchdogAcceptanceGUIBinary(t *testing.T) {
	// Minimal PE32+ headers exercise the real PE reader, including rejecting
	// the default console test build before it can launch interactive Tasks.
	image := make([]byte, 64+4+20+240)
	copy(image, "MZ")
	binary.LittleEndian.PutUint32(image[0x3c:], 64)
	copy(image[64:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(image[68:], pe.IMAGE_FILE_MACHINE_AMD64)
	binary.LittleEndian.PutUint16(image[84:], 240)
	binary.LittleEndian.PutUint16(image[88:], 0x20b)
	binary.LittleEndian.PutUint32(image[88+108:], 16)
	for _, test := range []struct {
		name      string
		subsystem uint16
		valid     bool
	}{
		{"gui", pe.IMAGE_SUBSYSTEM_WINDOWS_GUI, true},
		{"console", pe.IMAGE_SUBSYSTEM_WINDOWS_CUI, false},
		{"native", pe.IMAGE_SUBSYSTEM_NATIVE, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			binary.LittleEndian.PutUint16(image[88+68:], test.subsystem)
			if err := liveCheckGUIBinary(image); (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
	if err := liveCheckGUIBinary([]byte("not a PE image")); err == nil {
		t.Fatal("invalid image was accepted")
	}
}

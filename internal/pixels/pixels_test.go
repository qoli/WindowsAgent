package pixels

// Pixel conversion tests stay platform-independent so they can run on macOS.

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestBGRA8ToNRGBAHonorsRowPitch(t *testing.T) {
	src := []byte{
		10, 20, 30, 40, 50, 60, 70, 80, 0, 0, 0, 0,
		90, 100, 110, 120, 130, 140, 150, 160, 0, 0, 0, 0,
	}
	image, err := BGRA8ToNRGBA(src, 2, 2, 12)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		30, 20, 10, 40, 70, 60, 50, 80,
		110, 100, 90, 120, 150, 140, 130, 160,
	}
	if string(image.Pix) != string(want) {
		t.Fatalf("unexpected pixels: got %v want %v", image.Pix, want)
	}
}

func TestBGRA8ToNRGBARejectsInvalidBuffers(t *testing.T) {
	if _, err := BGRA8ToNRGBA(nil, 0, 1, 4); err != ErrInvalidDimensions {
		t.Fatalf("got %v, want %v", err, ErrInvalidDimensions)
	}
	if _, err := BGRA8ToNRGBA(make([]byte, 8), 2, 1, 4); err != ErrInvalidRowPitch {
		t.Fatalf("got %v, want %v", err, ErrInvalidRowPitch)
	}
	if _, err := BGRA8ToNRGBA(make([]byte, 7), 1, 2, 4); err != ErrShortBuffer {
		t.Fatalf("got %v, want %v", err, ErrShortBuffer)
	}
}

func TestHalfToFloat(t *testing.T) {
	tests := []struct {
		half uint16
		want float32
	}{
		{0x0000, 0},
		{0x3c00, 1},
		{0xc000, -2},
		{0x7c00, float32(math.Inf(1))},
		{0x0001, float32(math.Pow(2, -24))},
	}
	for _, test := range tests {
		got := HalfToFloat(test.half)
		if got != test.want {
			t.Fatalf("HalfToFloat(%#x) = %v, want %v", test.half, got, test.want)
		}
	}
}

func TestRGBA16FToneMappingPreservesHighlightOrdering(t *testing.T) {
	src := make([]byte, 16)
	putHalf(src[0:], 0x3c00)
	putHalf(src[2:], 0x3c00)
	putHalf(src[4:], 0x3c00)
	putHalf(src[6:], 0x3c00)
	putHalf(src[8:], 0x4400)
	putHalf(src[10:], 0x4400)
	putHalf(src[12:], 0x4400)
	putHalf(src[14:], 0x3c00)

	image, err := RGBA16FToToneMappedNRGBA(src, 2, 1, 16, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if image.Pix[4] <= image.Pix[0] {
		t.Fatalf("HDR highlight did not remain brighter: %v", image.Pix)
	}
	if image.Pix[4] == 255 {
		t.Fatalf("HDR highlight clipped to white: %v", image.Pix)
	}
}

func TestRGBA16FToneMappingRejectsMissingHDRMetadata(t *testing.T) {
	if _, err := RGBA16FToToneMappedNRGBA(make([]byte, 8), 1, 1, 8, 80); err != ErrInvalidHDR {
		t.Fatalf("got %v, want %v", err, ErrInvalidHDR)
	}
}

func TestValidatePackedSize(t *testing.T) {
	const width int32 = 1920
	const height int32 = 1080
	packed := uint64(uint32(width)) | uint64(uint32(height))<<32
	if err := ValidatePackedSize(width, height, packed); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePackedSize(width, height, packed+1); err == nil {
		t.Fatal("expected invalid ABI packing error")
	}
}

func putHalf(dst []byte, value uint16) {
	binary.LittleEndian.PutUint16(dst, value)
}

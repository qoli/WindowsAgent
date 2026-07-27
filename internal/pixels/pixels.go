// Package pixels converts WGC frame data into PNG-ready SDR pixels.
package pixels

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"
)

var (
	ErrInvalidDimensions = errors.New("invalid image dimensions")
	ErrInvalidRowPitch   = errors.New("row pitch is smaller than the image row")
	ErrShortBuffer       = errors.New("pixel buffer is shorter than row pitch times height")
	ErrInvalidHDR        = errors.New("HDR maximum luminance must be finite and greater than 80 nits")
)

func BGRA8ToNRGBA(src []byte, width, height, rowPitch int) (*image.NRGBA, error) {
	if width <= 0 || height <= 0 {
		return nil, ErrInvalidDimensions
	}
	rowBytes := width * 4
	if rowPitch < rowBytes {
		return nil, ErrInvalidRowPitch
	}
	if len(src) < rowPitch*height {
		return nil, ErrShortBuffer
	}

	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		srcRow := src[y*rowPitch:]
		dstRow := dst.Pix[y*dst.Stride:]
		for x := 0; x < width; x++ {
			s := x * 4
			d := x * 4
			dstRow[d] = srcRow[s+2]
			dstRow[d+1] = srcRow[s+1]
			dstRow[d+2] = srcRow[s]
			dstRow[d+3] = srcRow[s+3]
		}
	}
	return dst, nil
}

func RGBA16FToToneMappedNRGBA(src []byte, width, height, rowPitch int, maxLuminanceNits float64) (*image.NRGBA, error) {
	if width <= 0 || height <= 0 {
		return nil, ErrInvalidDimensions
	}
	rowBytes := width * 8
	if rowPitch < rowBytes {
		return nil, ErrInvalidRowPitch
	}
	if len(src) < rowPitch*height {
		return nil, ErrShortBuffer
	}
	if math.IsNaN(maxLuminanceNits) || math.IsInf(maxLuminanceNits, 0) || maxLuminanceNits <= 80 {
		return nil, ErrInvalidHDR
	}

	white := maxLuminanceNits / 80
	whiteSquared := white * white
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		srcRow := src[y*rowPitch:]
		dstRow := dst.Pix[y*dst.Stride:]
		for x := 0; x < width; x++ {
			s := x * 8
			r := clampNonNegative(float64(HalfToFloat(binary.LittleEndian.Uint16(srcRow[s:]))))
			g := clampNonNegative(float64(HalfToFloat(binary.LittleEndian.Uint16(srcRow[s+2:]))))
			b := clampNonNegative(float64(HalfToFloat(binary.LittleEndian.Uint16(srcRow[s+4:]))))

			luminance := 0.2126*r + 0.7152*g + 0.0722*b
			if luminance > 0 {
				mapped := luminance * (1 + luminance/whiteSquared) / (1 + luminance)
				scale := mapped / luminance
				r *= scale
				g *= scale
				b *= scale
			}

			d := x * 4
			dstRow[d] = linearToSRGB8(r)
			dstRow[d+1] = linearToSRGB8(g)
			dstRow[d+2] = linearToSRGB8(b)
			dstRow[d+3] = 255
		}
	}
	return dst, nil
}

func HalfToFloat(value uint16) float32 {
	sign := uint32(value>>15) << 31
	exponent := uint32(value>>10) & 0x1f
	mantissa := uint32(value & 0x03ff)

	switch exponent {
	case 0:
		if mantissa == 0 {
			return math.Float32frombits(sign)
		}
		exponent = 1
		for mantissa&0x0400 == 0 {
			mantissa <<= 1
			exponent--
		}
		mantissa &= 0x03ff
		exponent += 127 - 15
	case 0x1f:
		exponent = 0xff
	default:
		exponent += 127 - 15
	}

	return math.Float32frombits(sign | exponent<<23 | mantissa<<13)
}

func ValidatePackedSize(width, height int32, packed uint64) error {
	expected := uint64(uint32(width)) | uint64(uint32(height))<<32
	if packed != expected {
		return fmt.Errorf("invalid SizeInt32 ABI packing: got %#x, want %#x", packed, expected)
	}
	return nil
}

func clampNonNegative(value float64) float64 {
	if math.IsNaN(value) || value < 0 {
		return 0
	}
	return value
}

func linearToSRGB8(value float64) uint8 {
	value = math.Max(0, math.Min(1, value))
	var encoded float64
	if value <= 0.0031308 {
		encoded = 12.92 * value
	} else {
		encoded = 1.055*math.Pow(value, 1/2.4) - 0.055
	}
	return uint8(math.Round(encoded * 255))
}

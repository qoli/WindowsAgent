package pixels

import "errors"

// RGB24WordsToBGRXBottomUp converts top-down 0x00RRGGBB words returned by the
// WGC GPU sampling shader into the bottom-up BGRX32 layout used by Media
// Foundation. The conversion does not resample or reinterpret old frames.
func RGB24WordsToBGRXBottomUp(source []uint32, width, height int) ([]byte, error) {
	if width <= 0 || height <= 0 || len(source) != width*height {
		return nil, errors.New("RGB24 word dimensions are invalid")
	}
	output := make([]byte, len(source)*4)
	for y := 0; y < height; y++ {
		destination := (height - 1 - y) * width * 4
		for _, value := range source[y*width : (y+1)*width] {
			output[destination] = byte(value)
			output[destination+1] = byte(value >> 8)
			output[destination+2] = byte(value >> 16)
			output[destination+3] = 0xff
			destination += 4
		}
	}
	return output, nil
}

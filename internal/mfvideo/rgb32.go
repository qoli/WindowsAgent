package mfvideo

import (
	"errors"
	"image"
)

func rgb32ToNRGBA(source []byte, width, height, stride, cropX, cropY, cropWidth, cropHeight int) (*image.NRGBA, error) {
	rowStride := absolute(stride)
	if width < 1 || height < 1 || stride == 0 || rowStride < width*4 || cropX < 0 || cropY < 0 || cropWidth < 1 || cropHeight < 1 || cropX+cropWidth > width || cropY+cropHeight > height || len(source) < rowStride*height {
		return nil, errors.New("RGB32 decoded frame layout is invalid")
	}
	frame := image.NewNRGBA(image.Rect(0, 0, cropWidth, cropHeight))
	for y := 0; y < cropHeight; y++ {
		sourceY := cropY + y
		if stride < 0 {
			sourceY = height - 1 - sourceY
		}
		sourceOffset := sourceY*rowStride + cropX*4
		destinationOffset := y * frame.Stride
		for x := 0; x < cropWidth; x++ {
			frame.Pix[destinationOffset] = source[sourceOffset+2]
			frame.Pix[destinationOffset+1] = source[sourceOffset+1]
			frame.Pix[destinationOffset+2] = source[sourceOffset]
			frame.Pix[destinationOffset+3] = 0xff
			sourceOffset += 4
			destinationOffset += 4
		}
	}
	return frame, nil
}

func absolute(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

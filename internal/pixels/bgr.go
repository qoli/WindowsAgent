package pixels

import (
	"errors"
	"image"
	"math"
)

// NRGBAToBGR converts an SDR frame to packed BGR and bilinearly resizes it
// when the requested dimensions differ from the source.
func NRGBAToBGR(source *image.NRGBA, width, height int) ([]byte, error) {
	if source == nil || width <= 0 || height <= 0 || source.Bounds().Dx() <= 0 || source.Bounds().Dy() <= 0 {
		return nil, errors.New("invalid NRGBA-to-BGR dimensions")
	}
	sourceWidth, sourceHeight := source.Bounds().Dx(), source.Bounds().Dy()
	output := make([]byte, width*height*3)
	if width == sourceWidth && height == sourceHeight {
		for y := 0; y < height; y++ {
			sourceRow := source.Pix[y*source.Stride:]
			outputRow := output[y*width*3:]
			for x := 0; x < width; x++ {
				sourceOffset, outputOffset := x*4, x*3
				outputRow[outputOffset] = sourceRow[sourceOffset+2]
				outputRow[outputOffset+1] = sourceRow[sourceOffset+1]
				outputRow[outputOffset+2] = sourceRow[sourceOffset]
			}
		}
		return output, nil
	}

	for y := 0; y < height; y++ {
		sourceY := (float64(y)+0.5)*float64(sourceHeight)/float64(height) - 0.5
		y0 := max(0, min(sourceHeight-1, int(math.Floor(sourceY))))
		y1 := min(sourceHeight-1, y0+1)
		fy := max(0, min(1, sourceY-float64(y0)))
		for x := 0; x < width; x++ {
			sourceX := (float64(x)+0.5)*float64(sourceWidth)/float64(width) - 0.5
			x0 := max(0, min(sourceWidth-1, int(math.Floor(sourceX))))
			x1 := min(sourceWidth-1, x0+1)
			fx := max(0, min(1, sourceX-float64(x0)))
			for channel := 0; channel < 3; channel++ {
				top := float64(source.Pix[y0*source.Stride+x0*4+channel])*(1-fx) +
					float64(source.Pix[y0*source.Stride+x1*4+channel])*fx
				bottom := float64(source.Pix[y1*source.Stride+x0*4+channel])*(1-fx) +
					float64(source.Pix[y1*source.Stride+x1*4+channel])*fx
				output[(y*width+x)*3+(2-channel)] = byte(math.Round(top*(1-fy) + bottom*fy))
			}
		}
	}
	return output, nil
}

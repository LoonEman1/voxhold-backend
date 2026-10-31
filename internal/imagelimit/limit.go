// Package imagelimit bounds decoded image memory before allocating pixels.
package imagelimit

import (
	"image"
	"image/color"
	"math"
)

const MaxDecodedBytes int64 = 128 << 20

func DecodedBytes(config image.Config) int64 {
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width) > math.MaxInt64/int64(config.Height) {
		return math.MaxInt64
	}
	pixels := int64(config.Width) * int64(config.Height)
	bytes := int64(4)
	switch config.ColorModel {
	case color.GrayModel, color.AlphaModel:
		bytes = 1
	case color.YCbCrModel:
		bytes = 3
	case color.RGBA64Model, color.NRGBA64Model:
		bytes = 8
	case color.Gray16Model, color.Alpha16Model:
		bytes = 2
	default:
		if _, ok := config.ColorModel.(color.Palette); ok {
			bytes = 1
		}
	}
	if pixels > math.MaxInt64/bytes {
		return math.MaxInt64
	}
	return pixels * bytes
}

package imagelimit

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestDecodedBytes(t *testing.T) {
	for _, tt := range []struct {
		model color.Model
		bytes int64
	}{
		{color.GrayModel, 1}, {color.AlphaModel, 1}, {color.YCbCrModel, 3}, {color.RGBA64Model, 8}, {color.NRGBA64Model, 8},
		{color.Gray16Model, 2}, {color.Alpha16Model, 2}, {color.Palette{color.Black}, 1}, {color.RGBAModel, 4}, {color.NRGBAModel, 4}, {nil, 4},
	} {
		if got := DecodedBytes(image.Config{ColorModel: tt.model, Width: 10, Height: 20}); got != 200*tt.bytes {
			t.Fatalf("%T: %d", tt.model, got)
		}
	}
	for _, config := range []image.Config{{Width: 0, Height: 20}, {Width: -1, Height: 2}, {Width: math.MaxInt, Height: math.MaxInt}} {
		if got := DecodedBytes(config); got != math.MaxInt64 {
			t.Fatal(got)
		}
	}
}

package avatar

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestOptimizeCropsAndReencodes(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 800, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 800; x++ {
			source.Set(x, y, color.RGBA{R: 200, G: 30, A: 255})
		}
	}
	var input bytes.Buffer
	if err := png.Encode(&input, source); err != nil {
		t.Fatal(err)
	}
	encoded, err := Optimize(input.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != Size || decoded.Bounds().Dy() != Size {
		t.Fatalf("unexpected dimensions: %v", decoded.Bounds())
	}
	if len(encoded) > 131072 {
		t.Fatalf("avatar too large: %d", len(encoded))
	}
	r, g, _, _ := decoded.At(128, 128).RGBA()
	if r <= g {
		t.Fatal("crop did not preserve image color")
	}
}

func TestOptimizeRejectsInvalidAndOversizedInput(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("<svg/>"), make([]byte, MaxUploadBytes+1)} {
		if _, err := Optimize(data); err == nil {
			t.Fatal("invalid avatar accepted")
		}
	}
}

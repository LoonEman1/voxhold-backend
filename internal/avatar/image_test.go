package avatar

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
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
	decoded, err := png.Decode(bytes.NewReader(encoded))
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

func TestSelectedCircleDiscardsOutsidePixels(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 800, 400))
	for y := range 400 {
		for x := range 800 {
			c := color.NRGBA{R: 255, A: 255}
			if x >= 500 && x < 700 && y >= 100 && y < 300 {
				c = color.NRGBA{G: 220, A: 255}
			}
			source.SetNRGBA(x, y, c)
		}
	}
	var input bytes.Buffer
	if err := png.Encode(&input, source); err != nil {
		t.Fatal(err)
	}
	encoded, err := OptimizeWithCrop(input.Bytes(), &Crop{X: 500, Y: 100, Size: 200})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	r, g, _, a := decoded.At(128, 128).RGBA()
	if r != 0 || g == 0 || a != 65535 {
		t.Fatalf("wrong selected region: %d %d %d", r, g, a)
	}
	for _, p := range []image.Point{{0, 0}, {255, 0}, {0, 255}, {255, 255}, {10, 10}} {
		r, g, b, a := decoded.At(p.X, p.Y).RGBA()
		if r != 0 || g != 0 || b != 0 || a != 0 {
			t.Fatalf("outside pixels retained at %v", p)
		}
	}
	for _, crop := range []Crop{{-1, 0, 10}, {0, -1, 10}, {0, 0, 0}, {0, 0, -1}, {0, 0, 401}, {799, 0, 2}, {0, 399, 2}, {int(^uint(0) >> 1), 0, 10}} {
		if _, err := OptimizeWithCrop(input.Bytes(), &crop); !errors.Is(err, ErrCrop) {
			t.Fatalf("accepted invalid crop %v: %v", crop, err)
		}
	}
}

func TestHighEntropyAvatarFitsStorageAndRetainsTransparency(t *testing.T) {
	random := rand.New(rand.NewPCG(1, 2))
	source := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := range 256 {
		for x := range 256 {
			source.SetNRGBA(x, y, color.NRGBA{uint8(random.Uint32()), uint8(random.Uint32()), uint8(random.Uint32()), 255})
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
	if len(encoded) > 131072 {
		t.Fatalf("exceeds storage limit: %d", len(encoded))
	}
	decoded, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := decoded.At(0, 0).RGBA(); a != 0 {
		t.Fatal("outside circle is not transparent")
	}
}

func TestCropUsesEXIFOrientedCoordinates(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for y := range 200 {
		for x := range 400 {
			c := color.RGBA{R: 240, A: 255}
			if x >= 200 {
				c = color.RGBA{G: 240, A: 255}
			}
			source.Set(x, y, c)
		}
	}
	var original bytes.Buffer
	if err := jpeg.Encode(&original, source, nil); err != nil {
		t.Fatal(err)
	}
	// A real EXIF APP1 segment rotates the landscape JPEG 90 degrees clockwise.
	exif := []byte{'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	data := append([]byte{0xff, 0xd8, 0xff, 0xe1, 0, 0}, exif...)
	binary.BigEndian.PutUint16(data[4:6], uint16(len(exif)+2))
	data = append(data, original.Bytes()[2:]...)
	if jpegOrientation(data) != 6 {
		t.Fatal("orientation not read")
	}
	encoded, err := OptimizeWithCrop(data, &Crop{X: 0, Y: 200, Size: 200})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	r, g, _, _ := decoded.At(128, 128).RGBA()
	if g <= r {
		t.Fatal("crop does not match rotated preview")
	}
}

func TestAllEXIFOrientations(t *testing.T) {
	source := image.NewGray(image.Rect(0, 0, 2, 3))
	copy(source.Pix, []byte{1, 2, 3, 4, 5, 6})
	for index, want := range [][]byte{{1, 2, 3, 4, 5, 6}, {2, 1, 4, 3, 6, 5}, {6, 5, 4, 3, 2, 1}, {5, 6, 3, 4, 1, 2}, {1, 3, 5, 2, 4, 6}, {5, 3, 1, 6, 4, 2}, {6, 4, 2, 5, 3, 1}, {2, 4, 6, 1, 3, 5}} {
		oriented := orientedImage{Image: source, orientation: uint16(index + 1)}
		var got []byte
		for y := range oriented.Bounds().Dy() {
			for x := range oriented.Bounds().Dx() {
				got = append(got, color.GrayModel.Convert(oriented.At(x, y)).(color.Gray).Y)
			}
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("orientation %d: %v, want %v", index+1, got, want)
		}
	}
}

func TestOptimizeRejectsInvalidAndOversizedInput(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("<svg/>"), make([]byte, MaxUploadBytes+1)} {
		if _, err := Optimize(data); err == nil {
			t.Fatal("invalid avatar accepted")
		}
	}
}

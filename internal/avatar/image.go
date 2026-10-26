package avatar

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const MaxUploadBytes = 5 << 20
const MaxPixels = 16_000_000
const Size = 256

var ErrImage = errors.New("use a JPEG, PNG, WebP or GIF image up to 5 MB and 16 megapixels")

// DecodeConfig bounds allocation before decoding. Re-encoding strips metadata
// and animation; the database stores only the fixed-size derivative.
func Optimize(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > MaxUploadBytes {
		return nil, ErrImage
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > MaxPixels/config.Height {
		return nil, ErrImage
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrImage
	}
	bounds := source.Bounds()
	side := min(bounds.Dx(), bounds.Dy())
	x, y := bounds.Min.X+(bounds.Dx()-side)/2, bounds.Min.Y+(bounds.Dy()-side)/2
	crop := image.Rect(x, y, x+side, y+side)
	output := image.NewRGBA(image.Rect(0, 0, Size, Size))
	draw.Draw(output, output.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(output, output.Bounds(), source, crop, draw.Over, nil)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, output, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

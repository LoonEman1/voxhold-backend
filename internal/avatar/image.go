package avatar

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/color/palette"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const MaxUploadBytes = 5 << 20
const MaxPixels = 16_000_000
const Size = 256

var ErrImage = errors.New("use a JPEG, PNG, WebP or GIF image up to 5 MB and 16 megapixels")
var ErrCrop = errors.New("crop_x, crop_y and crop_size must describe a square inside the image")

// Crop uses pixels in the image after applying its EXIF orientation.
type Crop struct{ X, Y, Size int }

// DecodeConfig bounds allocation before decoding. Re-encoding strips metadata
// and animation; the database stores only the fixed-size derivative.
func Optimize(data []byte) ([]byte, error) {
	return OptimizeWithCrop(data, nil)
}

func OptimizeWithCrop(data []byte, selected *Crop) ([]byte, error) {
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
	source = orientedImage{Image: source, orientation: jpegOrientation(data)}
	bounds := source.Bounds()
	side := min(bounds.Dx(), bounds.Dy())
	x, y := bounds.Min.X+(bounds.Dx()-side)/2, bounds.Min.Y+(bounds.Dy()-side)/2
	if selected != nil {
		if selected.X < 0 || selected.Y < 0 || selected.Size <= 0 || selected.Size > side || selected.X > bounds.Dx()-selected.Size || selected.Y > bounds.Dy()-selected.Size {
			return nil, ErrCrop
		}
		x, y, side = selected.X, selected.Y, selected.Size
	}
	crop := image.Rect(x, y, x+side, y+side)
	output := image.NewNRGBA(image.Rect(0, 0, Size, Size))
	draw.CatmullRom.Scale(output, output.Bounds(), source, crop, draw.Src, nil)
	// Erase every pixel outside the selected circle, including its RGB data.
	// The one-pixel transition keeps the edge smooth at small display sizes.
	for y := range Size {
		for x := range Size {
			coverage := max(0, min(1, float64(Size)/2-math.Hypot(float64(x)+.5-Size/2, float64(y)+.5-Size/2)+.5))
			c := output.NRGBAAt(x, y)
			c.A = uint8(math.Round(float64(c.A) * coverage))
			if c.A == 0 {
				c = color.NRGBA{}
			}
			output.SetNRGBA(x, y, c)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, output); err != nil {
		return nil, err
	}
	if encoded.Len() > 131072 {
		// High-entropy images still fit the existing database limit. The last
		// palette entry retains fully transparent, erased corners.
		colors := append(color.Palette(nil), palette.Plan9[:255]...)
		colors = append(colors, color.Transparent)
		compact := image.NewPaletted(output.Bounds(), colors)
		draw.FloydSteinberg.Draw(compact, compact.Bounds(), output, image.Point{})
		encoded.Reset()
		if err := png.Encode(&encoded, compact); err != nil {
			return nil, err
		}
	}
	return encoded.Bytes(), nil
}

type orientedImage struct {
	image.Image
	orientation uint16
}

func (src orientedImage) Bounds() image.Rectangle {
	b := src.Image.Bounds()
	if src.orientation >= 5 && src.orientation <= 8 {
		return image.Rect(0, 0, b.Dy(), b.Dx())
	}
	return image.Rect(0, 0, b.Dx(), b.Dy())
}

func (src orientedImage) At(x, y int) color.Color {
	b := src.Image.Bounds()
	w, h := b.Dx(), b.Dy()
	switch src.orientation {
	case 2:
		x = w - 1 - x
	case 3:
		x, y = w-1-x, h-1-y
	case 4:
		y = h - 1 - y
	case 5:
		x, y = y, x
	case 6:
		x, y = y, h-1-x
	case 7:
		x, y = w-1-y, h-1-x
	case 8:
		x, y = w-1-y, x
	}
	return src.Image.At(b.Min.X+x, b.Min.Y+y)
}

func jpegOrientation(data []byte) uint16 {
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for offset := 2; offset+4 <= len(data); {
		if data[offset] != 0xff {
			break
		}
		marker := data[offset+1]
		if marker == 0xff {
			offset++
			continue
		}
		if marker == 0xda || marker == 0xd9 {
			break
		}
		size := int(binary.BigEndian.Uint16(data[offset+2:]))
		if size < 2 || size > len(data)-offset-2 {
			break
		}
		segment := data[offset+4 : offset+2+size]
		if marker == 0xe1 && len(segment) >= 14 && string(segment[:6]) == "Exif\x00\x00" {
			tiff := segment[6:]
			var order binary.ByteOrder
			switch string(tiff[:2]) {
			case "II":
				order = binary.LittleEndian
			case "MM":
				order = binary.BigEndian
			default:
				return 1
			}
			if order.Uint16(tiff[2:]) != 42 {
				return 1
			}
			start := uint64(order.Uint32(tiff[4:]))
			if start+2 > uint64(len(tiff)) {
				return 1
			}
			count := int(order.Uint16(tiff[start:]))
			for index, entry := 0, int(start)+2; index < count && entry+12 <= len(tiff); index, entry = index+1, entry+12 {
				if order.Uint16(tiff[entry:]) == 0x0112 && order.Uint16(tiff[entry+2:]) == 3 && order.Uint32(tiff[entry+4:]) == 1 {
					value := order.Uint16(tiff[entry+8:])
					if value >= 1 && value <= 8 {
						return value
					}
				}
			}
		}
		offset += 2 + size
	}
	return 1
}

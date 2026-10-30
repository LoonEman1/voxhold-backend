package attachments

import (
	"bytes"
	"encoding/binary"
	"golang.org/x/image/draw"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type noDecodeReader struct {
	*bytes.Reader
	t *testing.T
}

func (r noDecodeReader) Seek(offset int64, whence int) (int64, error) {
	r.t.Fatal("decoded an over-budget image")
	return 0, nil
}

func TestCompressImageRejects16BitMemoryBeforeDecode(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA64(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	header := data.Bytes()
	binary.BigEndian.PutUint32(header[16:20], 6000)
	binary.BigEndian.PutUint32(header[20:24], 4000)
	header[24], header[25] = 16, 6
	binary.BigEndian.PutUint32(header[29:33], crc32.ChecksumIEEE(header[12:29]))
	if _, _, err := compressImage(noDecodeReader{bytes.NewReader(header), t}); err == nil {
		t.Fatal("accepted 192 MB decoded pixels")
	}
}

func benchmarkPhoto(b *testing.B) []byte {
	b.Helper()
	img := image.NewYCbCr(image.Rect(0, 0, 6000, 4000), image.YCbCrSubsampleRatio420)
	for i := range img.Y {
		img.Y[i] = byte(i*13 + i/6000*29)
	}
	for i := range img.Cb {
		img.Cb[i] = 128
		img.Cr[i] = 128
	}
	var data bytes.Buffer
	if err := jpeg.Encode(&data, img, &jpeg.Options{Quality: 85}); err != nil {
		b.Fatal(err)
	}
	return data.Bytes()
}

func BenchmarkCompressImage(b *testing.B) {
	data := benchmarkPhoto(b)
	path := filepath.Join(b.TempDir(), "photo.jpg")
	if err := os.WriteFile(path, data, 0600); err != nil {
		b.Fatal(err)
	}
	for _, readAll := range []bool{true, false} {
		name := "File"
		if readAll {
			name = "LegacyReadAll"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				f, err := os.Open(path)
				if err != nil {
					b.Fatal(err)
				}
				var source io.ReadSeeker = f
				if readAll {
					copied, err := io.ReadAll(f)
					if err != nil {
						b.Fatal(err)
					}
					source = bytes.NewReader(copied)
				}
				out, _, err := compressImage(source)
				f.Close()
				if err != nil {
					b.Fatal(err)
				}
				config, _, err := image.DecodeConfig(bytes.NewReader(out))
				if err != nil || config.Width != 2560 || config.Height != 1706 {
					b.Fatalf("%v %v", config, err)
				}
			}
		})
	}
}

func BenchmarkImageScale(b *testing.B) {
	img := image.NewYCbCr(image.Rect(0, 0, 6000, 4000), image.YCbCrSubsampleRatio420)
	for _, filter := range []struct {
		name   string
		kernel *draw.Kernel
	}{{"CatmullRom", draw.CatmullRom}, {"BiLinear", draw.BiLinear}} {
		b.Run(filter.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				dst := image.NewNRGBA(image.Rect(0, 0, 2560, 1706))
				filter.kernel.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
			}
		})
	}
}

package utils

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-gui/goui/platform/typography"
)

func TestCopyBitmap(t *testing.T) {
	var src typography.TextBitmap
	src.Width = 100
	src.Height = 100
	src.Stride = src.Width * 4
	src.Pixels = make([]uint8, src.Stride*src.Height)

	red := makeLine(src.Stride, color.RGBA{R: 255, A: 255})
	green := makeLine(src.Stride, color.RGBA{G: 255, A: 255})
	for y := 0; y < src.Height; y++ {
		index := src.PixOffset(0, y)
		if y%2 == 0 {
			copy(src.Pixels[index:], red)
		} else {
			copy(src.Pixels[index:], green)
		}
	}

	savePng(t, "src.png", &src)

	dst := CopyBitmap(src, 80, 80, nil)
	savePng(t, "dst.png", &dst)
}

func TestReverseBitmap(t *testing.T) {
	var bitmap typography.TextBitmap
	bitmap.Width = 100
	bitmap.Height = 1
	bitmap.Stride = bitmap.Width * 4
	bitmap.Pixels = makeLine(bitmap.Stride, color.RGBA{R: 255, A: 255})
	savePng(t, "red.png", &bitmap)
	ReverseBitmap(bitmap)
	savePng(t, "blue.png", &bitmap)
}

func makeLine(stride int, c color.Color) []byte {
	line := make([]byte, stride)
	rgba := color.RGBAModel.Convert(c).(color.RGBA)
	for i := 0; i < len(line); i += 4 {
		line[i] = rgba.R
		line[i+1] = rgba.G
		line[i+2] = rgba.B
		line[i+3] = rgba.A
	}
	return line
}

// savePng writes a rendered image under the test's temporary directory. Writing
// it into the package directory turned every test run into untracked files.
func savePng(t *testing.T, filename string, img image.Image) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(t.TempDir(), filename), buf.Bytes(), 0666); err != nil {
		t.Fatal(err)
	}
}

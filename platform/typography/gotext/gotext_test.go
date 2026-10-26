package gotext

import (
	"image/color"
	"math"
	"os"
	"testing"
	"unicode/utf8"
	"unsafe"

	"github.com/golang-gui/goui/platform/typography"
	"golang.org/x/image/font/gofont/goregular"
)

// newGoContext builds a backend with the embedded Go font registered, so the
// tests do not depend on the machine's font set.
func newGoContext(t *testing.T) typography.Context {
	t.Helper()
	ctx, err := NewContext()
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	t.Cleanup(ctx.Destroy)
	adder, ok := ctx.(typography.FontDataAdder)
	if !ok {
		t.Fatalf("context does not implement FontDataAdder")
	}
	if err := adder.AddFontData(goregular.TTF); err != nil {
		t.Fatalf("AddFontData: %v", err)
	}
	if err := adder.AddFontData(goregular.TTF); err != nil {
		t.Fatalf("AddFontData repeat: %v", err)
	}
	return ctx
}

func baseFormat() typography.TextFormat {
	return typography.TextFormat{
		Font: typography.FontInfo{Family: "Go Regular", Size: 12},
	}
}

// isRuneBoundary reports whether b is a rune boundary in s, including the
// end-of-string position.
func isRuneBoundary(s string, b int) bool {
	return b == 0 || b == len(s) || (b < len(s) && utf8.RuneStart(s[b]))
}

// checkPremultiplied verifies every non-transparent pixel satisfies
// RGB <= A, the premultiplied contract of typography.TextBitmap.
func checkPremultiplied(t *testing.T, bmp typography.TextBitmap) {
	t.Helper()
	seen := 0
	for y := 0; y < bmp.Height; y++ {
		for x := 0; x < bmp.Width; x++ {
			i := y*bmp.Stride + 4*x
			a := bmp.Pixels[i+3]
			if a == 0 {
				continue
			}
			seen++
			if r, g, b := bmp.Pixels[i], bmp.Pixels[i+1], bmp.Pixels[i+2]; r > a || g > a || b > a {
				t.Fatalf("pixel (%d,%d) = %d,%d,%d,%d is not premultiplied", x, y, r, g, b, a)
			}
		}
	}
	if seen == 0 {
		t.Fatalf("bitmap %dx%d has no painted pixels", bmp.Width, bmp.Height)
	}
}

// inkBelow counts painted pixels from row yStart down, used to detect
// decorations drawn under the baseline.
func inkBelow(bmp typography.TextBitmap, yStart int) int {
	n := 0
	for y := max(0, yStart); y < bmp.Height; y++ {
		for x := 0; x < bmp.Width; x++ {
			if bmp.Pixels[y*bmp.Stride+4*x+3] != 0 {
				n++
			}
		}
	}
	return n
}

func TestMeasureAndClusters(t *testing.T) {
	ctx := newGoContext(t)
	layout, err := ctx.NewTextLayout("Hello 世界", baseFormat(), 0, 0)
	if err != nil {
		t.Fatalf("NewTextLayout: %v", err)
	}
	defer layout.Destroy()

	w, h := layout.MeasureSize()
	if w <= 0 || h <= 0 {
		t.Fatalf("MeasureSize = %v, %v; want positive", w, h)
	}

	lines, clusters := layout.MeasureMetrics()
	if len(lines) != 1 {
		t.Fatalf("lines = %d; want 1", len(lines))
	}
	if len(clusters) == 0 {
		t.Fatalf("no clusters")
	}
	text := layout.Text()
	seen := make(map[int]bool)
	for _, c := range clusters {
		if !isRuneBoundary(text, c.Start) {
			t.Fatalf("cluster start %d is not a rune boundary", c.Start)
		}
		if c.Start+c.Length > len(text) || !isRuneBoundary(text, c.Start+c.Length) {
			t.Fatalf("cluster end %d+%d is not a rune boundary", c.Start, c.Length)
		}
		if c.Width <= 0 {
			t.Errorf("cluster at %d has width %v", c.Start, c.Width)
		}
		seen[c.Start] = true
	}
	// Every rune of the mixed text must be covered by exactly one cluster.
	for b := 0; b < len(text); {
		_, n := utf8.DecodeRuneInString(text[b:])
		if !seen[b] {
			t.Fatalf("byte offset %d (%q) not covered by any cluster", b, text[b:b+n])
		}
		b += n
	}

	bitmap, err := layout.Rasterize(1, nil)
	if err != nil {
		t.Fatalf("Rasterize: %v", err)
	}
	if bitmap.Width <= 0 || bitmap.Height <= 0 || len(bitmap.Pixels) == 0 {
		t.Fatalf("bitmap = %dx%d len %d; want non-empty", bitmap.Width, bitmap.Height, len(bitmap.Pixels))
	}
	checkPremultiplied(t, bitmap)
}

func TestColorAndUnderlinePixels(t *testing.T) {
	ctx := newGoContext(t)
	layout, err := ctx.NewTextLayout("Hello", baseFormat(), 0, 0)
	if err != nil {
		t.Fatalf("NewTextLayout: %v", err)
	}
	defer layout.Destroy()
	lines, _ := layout.MeasureMetrics()
	if len(lines) != 1 {
		t.Fatalf("lines = %d; want 1", len(lines))
	}
	// Rows strictly below the baseline's own row start empty for "Hello",
	// which has no descenders; the underline must add ink there.
	belowStart := int(math.Floor(float64(lines[0].Baseline))) + 1

	plain, err := layout.Rasterize(1, nil)
	if err != nil {
		t.Fatalf("Rasterize plain: %v", err)
	}
	if ink := inkBelow(plain, belowStart); ink != 0 {
		t.Fatalf("plain bitmap has %d pixels below the baseline row", ink)
	}

	red := color.RGBA{255, 0, 0, 255}
	layout.SetTextColor(0, len("Hello"), red)
	colored, err := layout.Rasterize(1, nil)
	if err != nil {
		t.Fatalf("Rasterize colored: %v", err)
	}
	found := false
	for y := 0; y < colored.Height && !found; y++ {
		for x := 0; x < colored.Width; x++ {
			i := y*colored.Stride + 4*x
			if colored.Pixels[i] == 255 && colored.Pixels[i+1] == 0 &&
				colored.Pixels[i+2] == 0 && colored.Pixels[i+3] == 255 {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("no fully covered pixel carries the requested red color")
	}

	layout.SetUnderline(0, len("Hello"), true)
	underlined, err := layout.Rasterize(1, nil)
	if err != nil {
		t.Fatalf("Rasterize underlined: %v", err)
	}
	if ink := inkBelow(underlined, belowStart); ink == 0 {
		t.Fatalf("underline added no pixels below the baseline row")
	}
	checkPremultiplied(t, underlined)
}

func TestRasterizeBufferReuse(t *testing.T) {
	ctx := newGoContext(t)
	layout, err := ctx.NewTextLayout("Hello", baseFormat(), 0, 0)
	if err != nil {
		t.Fatalf("NewTextLayout: %v", err)
	}
	defer layout.Destroy()

	w, h := layout.MeasureSize()
	need := int(math.Ceil(float64(w))) * 4 * int(math.Ceil(float64(h)))
	buf := make([]byte, 0, need)
	bitmap, err := layout.Rasterize(1, buf)
	if err != nil {
		t.Fatalf("Rasterize: %v", err)
	}
	if bitmap.Stride*bitmap.Height > cap(buf) {
		t.Fatalf("required size %d exceeds buffer capacity %d", bitmap.Stride*bitmap.Height, cap(buf))
	}
	if unsafe.SliceData(bitmap.Pixels) != unsafe.SliceData(buf) {
		t.Fatalf("bitmap did not reuse the caller buffer backing array")
	}

	small := make([]byte, 0, 4)
	bitmap, err = layout.Rasterize(1, small)
	if err != nil {
		t.Fatalf("Rasterize small: %v", err)
	}
	if unsafe.SliceData(bitmap.Pixels) == unsafe.SliceData(small) {
		t.Fatalf("bitmap reused an undersized buffer")
	}
	checkPremultiplied(t, bitmap)
}

func TestWrapOnNarrowSize(t *testing.T) {
	ctx := newGoContext(t)
	format := baseFormat()
	layout, err := ctx.NewTextLayout("The quick brown fox jumps over the lazy dog", format, 1000, 0)
	if err != nil {
		t.Fatalf("NewTextLayout: %v", err)
	}
	defer layout.Destroy()
	layout.SetWrapMode(typography.WrapWordChar)

	before, _ := layout.MeasureMetrics()
	if len(before) != 1 {
		t.Fatalf("wide layout lines = %d; want 1", len(before))
	}

	layout.SetSize(60, 0)
	lines, _ := layout.MeasureMetrics()
	if len(lines) < 2 {
		t.Fatalf("narrow layout lines = %d; want more than 1", len(lines))
	}
	text := layout.Text()
	covered := 0
	for i, ln := range lines {
		if ln.Start+ln.Length > len(text) {
			t.Fatalf("line %d out of range", i)
		}
		if i > 0 && ln.Start != lines[i-1].Start+lines[i-1].Length {
			t.Fatalf("line %d starts at %d, previous ends at %d; gap or overlap", i, ln.Start, lines[i-1].Start+lines[i-1].Length)
		}
		covered += ln.Length
	}
	if covered != len(text) {
		t.Fatalf("lines cover %d of %d bytes", covered, len(text))
	}
}

func TestChangedSignalSynchronous(t *testing.T) {
	ctx := newGoContext(t)
	layout, err := ctx.NewTextLayout("abc", baseFormat(), 10, 0)
	if err != nil {
		t.Fatalf("NewTextLayout: %v", err)
	}
	defer layout.Destroy()

	calls := 0
	handle := layout.ConnectChanged(func() { calls++ })
	defer handle.Disconnect()

	layout.SetWrapMode(typography.WrapChar)
	if calls == 0 {
		t.Fatalf("changed signal not emitted synchronously")
	}
	// A no-op update must not notify.
	before := calls
	layout.SetWrapMode(typography.WrapChar)
	if calls != before {
		t.Fatalf("no-op SetWrapMode emitted %d notifications", calls-before)
	}
}

func TestDestroyIdempotent(t *testing.T) {
	ctx := newGoContext(t)
	layout, err := ctx.NewTextLayout("abc", baseFormat(), 0, 0)
	if err != nil {
		t.Fatalf("NewTextLayout: %v", err)
	}
	destroyed := 0
	layout.ConnectDestroy(func() { destroyed++ })
	layout.Destroy()
	layout.Destroy()
	if destroyed != 1 {
		t.Fatalf("destroy notifications = %d; want 1", destroyed)
	}
}

func TestEmptyTextRasterize(t *testing.T) {
	ctx := newGoContext(t)
	layout, err := ctx.NewTextLayout("", baseFormat(), 0, 0)
	if err != nil {
		t.Fatalf("NewTextLayout: %v", err)
	}
	defer layout.Destroy()
	bitmap, err := layout.Rasterize(1, nil)
	if err != nil {
		t.Fatalf("Rasterize empty: %v", err)
	}
	if bitmap.Width != 0 || bitmap.Height != 0 {
		t.Fatalf("empty bitmap = %dx%d; want 0x0", bitmap.Width, bitmap.Height)
	}
}

// TestChineseSystemFont verifies CJK coverage through a real installed font.
// It is skipped when the font file is absent.
func TestChineseSystemFont(t *testing.T) {
	ctx, err := NewContext()
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	defer ctx.Destroy()
	const path = `C:\Windows\Fonts\msyh.ttc`
	if _, err := os.Stat(path); err != nil {
		t.Skipf("system font unavailable: %v", err)
	}
	if err := ctx.AddFont(path); err != nil {
		t.Fatalf("AddFont: %v", err)
	}
	format := typography.TextFormat{Font: typography.FontInfo{Family: "Microsoft YaHei", Size: 12}}
	layout, err := ctx.NewTextLayout("中文测试", format, 0, 0)
	if err != nil {
		t.Fatalf("NewTextLayout: %v", err)
	}
	defer layout.Destroy()
	w, h := layout.MeasureSize()
	if w <= 0 || h <= 0 {
		t.Fatalf("MeasureSize = %v, %v; want positive", w, h)
	}
}

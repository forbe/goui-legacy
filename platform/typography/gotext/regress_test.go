package gotext

import (
	"fmt"
	"image/color"
	"strings"
	"sync"
	"testing"

	"github.com/golang-gui/goui/platform/typography"
	"golang.org/x/image/font/gofont/goregular"
)

// Every case here ends a paragraph with a hard break, including the empty ones.
// The trailing paragraph has no runes, and indexing the per-rune font table for
// it used to panic inside NewTextLayout.
func TestTrailingAndEmptyParagraphs(t *testing.T) {
	ctx := newGoContext(t)
	oneLine, err := ctx.NewTextLayout("abc", baseFormat(), 80, 200)
	if err != nil {
		t.Fatalf("NewTextLayout: %v", err)
	}
	_, lineHeight := oneLine.MeasureSize()
	if lineHeight <= 0 {
		t.Fatalf("single line height = %v", lineHeight)
	}
	for _, text := range []string{"abc\n", "\n", "\n\n", "a\nb\n", "a\n\nb"} {
		layout, err := ctx.NewTextLayout(text, baseFormat(), 80, 200)
		if err != nil {
			t.Fatalf("%q: NewTextLayout: %v", text, err)
		}
		width, height := layout.MeasureSize()
		lines, _ := layout.MeasureMetrics()
		if width < 0 {
			t.Errorf("%q: width = %v", text, width)
		}
		if want := strings.Count(text, "\n") + 1; len(lines) != want {
			t.Errorf("%q: %d lines, want %d", text, len(lines), want)
		}
		// Every line starts below the previous one and the total height covers all
		// of them; a paragraph that restarts at y=0 collapses the text.
		if want := float32(len(lines)) * lineHeight; height < want*0.9 {
			t.Errorf("%q: height = %v, want at least %v over %d lines", text, height, want*0.9, len(lines))
		}
		for i, line := range lines {
			if i > 0 && line.Y <= lines[i-1].Y {
				t.Errorf("%q line %d: Y = %v does not follow %v", text, i, line.Y, lines[i-1].Y)
			}
			if line.Start+line.Length > len(text) {
				t.Errorf("%q line %d: range %d+%d exceeds text", text, i, line.Start, line.Length)
			}
		}
		if _, err := layout.Rasterize(1, nil); err != nil {
			t.Errorf("%q: Rasterize: %v", text, err)
		}
	}
}

// The DirectWrite backend ignores empty and out-of-range decorations instead of
// failing the frame, so the fallback must not panic on them either.
func TestDegenerateRangesIgnored(t *testing.T) {
	ctx := newGoContext(t)
	layout, err := ctx.NewTextLayout("hello", baseFormat(), 0, 0)
	if err != nil {
		t.Fatalf("NewTextLayout: %v", err)
	}
	text := layout.(*textLayout)
	for _, r := range [][2]int{{0, 0}, {-1, 5}, {5, 1}, {0, 6}, {2, -3}} {
		text.SetTextColor(r[0], r[1], color.RGBA{A: 255})
		text.SetUnderline(r[0], r[1], true)
		text.SetStrikethrough(r[0], r[1], true)
	}
	if len(text.colors)+len(text.decorations) != 0 {
		t.Fatalf("degenerate ranges recorded: %d colors, %d decorations",
			len(text.colors), len(text.decorations))
	}
	// Fonts keep the shared validator, which panics like DirectWrite does.
	func() {
		defer func() {
			if recover() == nil {
				t.Errorf("SetTextFont(0,0) accepted an empty range")
			}
		}()
		text.SetTextFont(0, 0, typography.FontInfo{Family: "Go Regular", Size: 12})
	}()
}

// A whole-text range supersedes every earlier one.
func TestFullRangeCollapsesEarlierRanges(t *testing.T) {
	ctx := newGoContext(t)
	layout, err := ctx.NewTextLayout("hello", baseFormat(), 0, 0)
	if err != nil {
		t.Fatalf("NewTextLayout: %v", err)
	}
	text := layout.(*textLayout)
	red := color.RGBA{R: 255, A: 255}
	for i := 0; i < 5; i++ {
		text.SetTextColor(0, 2, red)
	}
	if len(text.colors) != 5 {
		t.Fatalf("partial ranges = %d, want 5 kept", len(text.colors))
	}
	text.SetTextColor(0, len(text.text), red)
	if len(text.colors) != 1 {
		t.Fatalf("after a full-range request colors = %d, want 1", len(text.colors))
	}
	text.SetTextColor(1, 2, red)
	if len(text.colors) != 2 {
		t.Fatalf("later partial range dropped: colors = %d, want 2", len(text.colors))
	}
}

// MeasureMetrics must not hand back the buffers relayout recycles in place.
func TestMeasureMetricsOutlivesRelayout(t *testing.T) {
	ctx := newGoContext(t)
	format := baseFormat()
	format.WrapMode = typography.WrapWordChar
	layout, err := ctx.NewTextLayout("hello world", format, 200, 0)
	if err != nil {
		t.Fatalf("NewTextLayout: %v", err)
	}
	_, firstHeight := layout.MeasureSize()
	lines, clusters := layout.MeasureMetrics()
	if len(lines) != 1 || len(clusters) == 0 {
		t.Fatalf("lines = %d, clusters = %d", len(lines), len(clusters))
	}
	firstCluster := clusters[0]

	layout.SetSize(20, 0)
	_, secondHeight := layout.MeasureSize()
	if secondHeight <= firstHeight {
		t.Fatalf("narrower box did not add lines: %v then %v", firstHeight, secondHeight)
	}
	if clusters[0] != firstCluster {
		t.Fatalf("returned clusters were rewritten by relayout: %+v then %+v", firstCluster, clusters[0])
	}
	if len(lines[0].Clusters) != len(clusters) {
		t.Fatalf("returned line kept %d clusters, the text has %d", len(lines[0].Clusters), len(clusters))
	}
}

// The rasterizer reads its source as premultiplied, so a translucent color has
// to reach the bitmap premultiplied. Straight channels would come out too
// bright and break the RGB <= A contract of TextBitmap.
func TestTranslucentColorIsPremultiplied(t *testing.T) {
	ctx := newGoContext(t)
	layout, err := ctx.NewTextLayout("Hello", baseFormat(), 0, 0)
	if err != nil {
		t.Fatalf("NewTextLayout: %v", err)
	}
	defer layout.Destroy()
	layout.SetTextColor(0, len("Hello"), color.RGBA{R: 255, A: 128})
	bitmap, err := layout.Rasterize(1, nil)
	if err != nil {
		t.Fatalf("Rasterize: %v", err)
	}
	best := byte(0)
	for y := 0; y < bitmap.Height; y++ {
		for x := 0; x < bitmap.Width; x++ {
			i := y*bitmap.Stride + 4*x
			a := bitmap.Pixels[i+3]
			if a == 0 {
				continue
			}
			if r := bitmap.Pixels[i]; r > a {
				t.Fatalf("pixel (%d,%d) = %d,%d,%d,%d is not premultiplied",
					x, y, r, bitmap.Pixels[i+1], bitmap.Pixels[i+2], a)
			}
			if a > best {
				best = a
			}
		}
	}
	if best == 0 {
		t.Fatal("no painted pixels")
	}
	if best < 120 || best > 128 {
		t.Fatalf("strongest pixel alpha = %d, want the requested 128", best)
	}
}

// The destroy notification runs application callbacks, so it must go out
// without the context lock: a subscriber that uses the context again would
// deadlock on it.
func TestDestroyNotificationCanUseContext(t *testing.T) {
	ctx := newGoContext(t)
	layout, err := ctx.NewTextLayout("hello", baseFormat(), 0, 0)
	if err != nil {
		t.Fatalf("NewTextLayout: %v", err)
	}
	built := 0
	handle := layout.ConnectDestroy(func() {
		other, err := ctx.NewTextLayout("world", baseFormat(), 0, 0)
		if err != nil {
			t.Errorf("NewTextLayout in destroy handler: %v", err)
			return
		}
		built++
		other.Destroy()
	})
	defer handle.Disconnect()
	layout.Destroy()
	if built != 1 {
		t.Fatalf("destroy handler ran %d times, want 1", built)
	}
}

// Use after Destroy reports an error rather than dereferencing the font map.
func TestDestroyedContextReportsErrors(t *testing.T) {
	ctx, err := NewContext()
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	ctx.(typography.FontDataAdder).AddFontData(goregular.TTF)
	layout, err := ctx.NewTextLayout("hello", baseFormat(), 0, 0)
	if err != nil {
		t.Fatalf("NewTextLayout: %v", err)
	}
	ctx.Destroy()

	if err := ctx.(typography.FontDataAdder).AddFontData(goregular.TTF); err == nil {
		t.Errorf("AddFontData after Destroy: nil error")
	}
	if _, err := ctx.NewTextLayout("hello", baseFormat(), 0, 0); err == nil {
		t.Errorf("NewTextLayout after Destroy: nil error")
	}
	if _, err := layout.Rasterize(1, nil); err == nil {
		t.Errorf("Rasterize after context Destroy: nil error")
	}
	layout.MeasureSize()
}

// Font registration and shaping share the context's font map, and go-text hands
// out faces that every layout of the context reuses, so an off-thread call must
// serialize instead of corrupting the map. Run this under -race.
func TestConcurrentShapingAndRegistration(t *testing.T) {
	ctx := newGoContext(t)
	adder := ctx.(typography.FontDataAdder)
	ink := color.RGBA{R: 10, G: 20, B: 30, A: 255}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			layout, err := ctx.NewTextLayout(fmt.Sprintf("thread %d\nsecond line", i), baseFormat(), 100, 0)
			if err != nil {
				t.Errorf("goroutine %d: NewTextLayout: %v", i, err)
				return
			}
			layout.SetTextColor(0, 5, ink)
			layout.SetSize(120, 0)
			layout.MeasureSize()
			lines, clusters := layout.MeasureMetrics()
			if len(lines) == 0 || len(clusters) == 0 {
				t.Errorf("goroutine %d: empty metrics: %d lines, %d clusters", i, len(lines), len(clusters))
			}
			if _, err := layout.Rasterize(2, nil); err != nil {
				t.Errorf("goroutine %d: Rasterize: %v", i, err)
			}
			if err := adder.AddFontData(goregular.TTF); err != nil {
				t.Errorf("goroutine %d: AddFontData: %v", i, err)
			}
			layout.Destroy()
		}(i)
	}
	wg.Wait()
}

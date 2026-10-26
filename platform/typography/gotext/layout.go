package gotext

import (
	"fmt"
	"image/color"
	"math"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/vector"

	"github.com/golang-gui/goui/core/signal"
	"github.com/golang-gui/goui/platform/typography"
	"github.com/golang-gui/goui/platform/typography/internal/lifecycle"
	"github.com/golang-gui/goui/platform/typography/utils"
)

// fontRange carries a formatting override applied to a UTF-8 byte range.
// Start and Length are byte offsets into the layout text on rune boundaries.
type fontRange struct {
	Start  int
	Length int
	Font   typography.FontInfo
}

type colorRange struct {
	Start  int
	Length int
	Color  color.RGBA
}

type decorationRange struct {
	Start  int
	Length int
	Under  bool
	Strike bool
}

type textLayout struct {
	ctx       *context
	life      lifecycle.Layout
	text      string
	format    typography.TextFormat
	maxWidth  float32
	maxHeight float32

	fonts        []fontRange
	colors       []colorRange
	decorations  []decorationRange
	placeholders map[int]typography.PlaceholderMetrics

	// defaultTabWidth emulates the backend default stop used when TabWidth is 0.
	defaultTabWidth float32
	dirty           bool

	// Shaping objects kept across relayouts. Upstream documents all three as
	// reusable and cheaper to reuse than to rebuild, and they hold pooling state
	// rather than results, so a fresh paragraph never sees stale output.
	shaper    shaping.HarfbuzzShaper
	segmenter shaping.Segmenter
	wrapper   shaping.LineWrapper
	// rasterizer is the shared glyph outline filler, resized per raster pass.
	rasterizer *vector.Rasterizer

	// relayout results, valid while dirty is false.
	occupiedWidth  float32
	occupiedHeight float32
	lines          []typography.TextLine
	clusters       []typography.TextCluster

	// rasterGlyphs lists the visible glyphs with their pen positions in
	// logical units, collected during buildLine for the raster pass.
	rasterGlyphs []rasterGlyph
	// lineFaces and lineEms carry the face and em size of each line, used to
	// resolve underline and strikethrough metrics.
	lineFaces []*font.Face
	lineEms   []float32
}

func newTextLayout(ctx *context, text string, format typography.TextFormat, width, height float32) *textLayout {
	t := &textLayout{
		ctx:       ctx,
		text:      text,
		format:    format,
		maxWidth:  width,
		maxHeight: height,
		dirty:     true,
	}
	resolved, _ := utils.ResolveFont(format.Font)
	// The default stop is three em of the base font, matching the fallback
	// DirectWrite uses when no explicit tab width is requested.
	t.defaultTabWidth = ptToDip(resolved.Size) * 3
	t.format.TabWidth = -1
	t.setTabWidth(format.TabWidth)
	return t
}

func (t *textLayout) Destroy() {
	t.ctx.mu.Lock()
	if t.life.IsDestroyed() {
		t.ctx.mu.Unlock()
		return
	}
	// Release what the layout pins from the shared font map: a retained
	// destroyed layout would otherwise hold every face it ever resolved.
	t.lines = nil
	t.clusters = nil
	t.rasterGlyphs = nil
	t.lineFaces = nil
	t.lineEms = nil
	t.rasterizer = nil
	t.fonts = nil
	t.colors = nil
	t.decorations = nil
	t.placeholders = nil
	t.ctx.mu.Unlock()
	// The notification runs application callbacks, so it goes out without the
	// context lock: a subscriber that touches the context again would
	// deadlock on it. The DirectWrite backend releases its native state the
	// same way.
	t.life.BeginDestroy()
}

func (t *textLayout) Text() string {
	return t.text
}

func (t *textLayout) Format() typography.TextFormat {
	t.ctx.mu.Lock()
	defer t.ctx.mu.Unlock()
	return t.format
}

func (t *textLayout) Size() (maxWidth, maxHeight float32) {
	t.ctx.mu.Lock()
	defer t.ctx.mu.Unlock()
	return t.maxWidth, t.maxHeight
}

func (t *textLayout) SetSize(maxWidth, maxHeight float32) {
	t.edit(func() bool {
		if t.maxWidth == maxWidth && t.maxHeight == maxHeight {
			return false
		}
		t.maxWidth, t.maxHeight = maxWidth, maxHeight
		return true
	})
}

func (t *textLayout) SetTextAlignment(align typography.TextAlignment) {
	t.edit(func() bool {
		if t.format.TextAlign == align {
			return false
		}
		t.format.TextAlign = align
		return true
	})
}

func (t *textLayout) SetWrapMode(wrap typography.WrapMode) {
	t.edit(func() bool {
		if t.format.WrapMode == wrap {
			return false
		}
		t.format.WrapMode = wrap
		return true
	})
}

func (t *textLayout) SetTabWidth(width float32) {
	if err := utils.ValidateTabWidth(width); err != nil {
		panic(err)
	}
	t.edit(func() bool { return t.setTabWidth(width) })
}

// setTabWidth applies the requested stop and reports whether it changed. The
// constructor uses it without the edit wrapper because it builds an already
// dirty layout.
func (t *textLayout) setTabWidth(width float32) bool {
	if t.format.TabWidth == width {
		return false
	}
	t.format.TabWidth = width
	return true
}

func (t *textLayout) tabStop() float32 {
	if t.format.TabWidth > 0 {
		return t.format.TabWidth
	}
	return t.defaultTabWidth
}

// validRange reports whether start..start+length selects a nonempty byte range
// inside the text. The DirectWrite backend ignores out-of-range and empty
// requests instead of failing the frame, so the fallback must not panic on them.
func (t *textLayout) validRange(start, length int) bool {
	return 0 <= start && 0 < length && start+length <= len(t.text)
}

func (t *textLayout) SetTextFont(start, length int, font typography.FontInfo) {
	// Range and font validation stay a panic here because the DirectWrite backend
	// panics the same way before touching the native layout.
	utils.CheckTextRange(t.text, start, length)
	resolved, err := utils.ResolveFont(font)
	if err != nil {
		panic(err)
	}
	t.edit(func() bool {
		if start == 0 && length == len(t.text) {
			t.fonts = t.fonts[:0]
		}
		t.fonts = append(t.fonts, fontRange{Start: start, Length: length, Font: resolved})
		return true
	})
}

func (t *textLayout) SetTextColor(start, length int, c color.Color) {
	t.edit(func() bool {
		if !t.validRange(start, length) {
			return false
		}
		// A full foreground replacement makes every older range obsolete; keeping
		// them would grow memory and the per-cluster scan on every recolor.
		if start == 0 && length == len(t.text) {
			t.colors = t.colors[:0]
		}
		t.colors = append(t.colors, colorRange{Start: start, Length: length, Color: toRGBAColor(c)})
		return true
	})
}

func (t *textLayout) SetUnderline(start, length int, underline bool) {
	t.edit(func() bool {
		if !t.validRange(start, length) {
			return false
		}
		t.setDecoration(start, length, underline, false)
		return true
	})
}

func (t *textLayout) SetStrikethrough(start, length int, strike bool) {
	t.edit(func() bool {
		if !t.validRange(start, length) {
			return false
		}
		t.setDecoration(start, length, false, strike)
		return true
	})
}

func (t *textLayout) setDecoration(start, length int, underline, strike bool) {
	for i := range t.decorations {
		if t.decorations[i].Start == start && t.decorations[i].Length == length {
			if underline {
				t.decorations[i].Under = true
			}
			if strike {
				t.decorations[i].Strike = true
			}
			return
		}
	}
	d := decorationRange{Start: start, Length: length}
	if underline {
		d.Under = true
	}
	if strike {
		d.Strike = true
	}
	t.decorations = append(t.decorations, d)
}

func (t *textLayout) SetPlaceholder(start int, metrics *typography.PlaceholderMetrics) {
	utils.CheckPlaceholder(t.text, start, metrics)
	t.edit(func() bool {
		if metrics == nil {
			delete(t.placeholders, start)
		} else {
			if t.placeholders == nil {
				t.placeholders = make(map[int]typography.PlaceholderMetrics)
			}
			t.placeholders[start] = *metrics
		}
		return true
	})
}

func (t *textLayout) MeasureSize() (width, height float32) {
	t.ctx.mu.Lock()
	defer t.ctx.mu.Unlock()
	t.relayout()
	return t.occupiedWidth, t.occupiedHeight
}

func (t *textLayout) MeasureMetrics() (lines []typography.TextLine, clusters []typography.TextCluster) {
	t.ctx.mu.Lock()
	defer t.ctx.mu.Unlock()
	t.relayout()
	// relayout recycles t.lines and t.clusters in place, and each line's Clusters
	// aliases t.clusters, so callers get copies they may keep across a resize.
	clusters = append(make([]typography.TextCluster, 0, len(t.clusters)), t.clusters...)
	lines = append(make([]typography.TextLine, 0, len(t.lines)), t.lines...)
	for i := range lines {
		lines[i].Clusters = append(make([]typography.TextCluster, 0, len(lines[i].Clusters)), lines[i].Clusters...)
	}
	return lines, clusters
}

func (t *textLayout) Rasterize(scale float32, buf []byte) (bitmap typography.TextBitmap, err error) {
	if t.life.IsDestroyed() {
		return bitmap, fmt.Errorf("gotext: rasterize destroyed text layout")
	}
	if scale <= 0 || math.IsNaN(float64(scale)) || math.IsInf(float64(scale), 0) {
		return bitmap, fmt.Errorf("gotext: invalid raster scale %v", scale)
	}
	t.ctx.mu.Lock()
	defer t.ctx.mu.Unlock()
	if t.ctx.destroyed {
		return bitmap, fmt.Errorf("gotext: rasterize after the typography context was destroyed")
	}
	return t.rasterizeGlyphs(scale, buf)
}

func (t *textLayout) ConnectChanged(fn func()) signal.Handle {
	return t.life.ConnectChanged(fn)
}

func (t *textLayout) ConnectDestroy(fn func()) signal.Handle {
	return t.life.ConnectDestroy(fn)
}

// edit serializes a formatting change on the context lock, marks the layout
// dirty and notifies subscribers. Reshaping stays lazy: k setters in one build
// pass cost one shape, not k. The notification fires outside the lock because
// it runs application callbacks.
func (t *textLayout) edit(fn func() bool) {
	t.ctx.mu.Lock()
	changed := fn()
	if changed {
		t.dirty = true
	}
	t.ctx.mu.Unlock()
	if changed {
		t.life.Changed()
	}
}

// toRGBAColor converts c to the premultiplied RGBA that TextBitmap requires.
// The color is read as straight alpha, the way the DirectWrite backend reads it
// for its brush, and then premultiplied here: the rasterizer and image/draw
// both read the channels as premultiplied, so a translucent color.RGBA would
// otherwise come out too bright and break the RGB <= A contract.
func toRGBAColor(c color.Color) color.RGBA {
	if c == nil {
		c = typography.DefaultTextColor()
	}
	rgba := color.NRGBA64Model.Convert(c).(color.NRGBA64)
	if rgba.A == 0 {
		return color.RGBA{}
	}
	return color.RGBA{
		R: uint8(uint32(rgba.R) * uint32(rgba.A) / 0xffff >> 8),
		G: uint8(uint32(rgba.G) * uint32(rgba.A) / 0xffff >> 8),
		B: uint8(uint32(rgba.B) * uint32(rgba.A) / 0xffff >> 8),
		A: uint8(rgba.A >> 8),
	}
}

func ptToDip(pt float32) float32 {
	// Font.Size is in typographic points (1 pt = 1/72 inch); layout units are
	// logical DIPs (1 DIP = 1/96 inch), so 1 pt = 96/72 DIP.
	return pt * 96.0 / 72.0
}

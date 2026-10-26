package gotext

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"golang.org/x/image/vector"

	"github.com/golang-gui/goui/platform/typography"
)

// rasterGlyph is a visible glyph placed by the line walk in logical units,
// collected during shaping and consumed by the pixel pass.
type rasterGlyph struct {
	face *font.Face
	gid  font.GID
	x, y float32 // Pen position: x is the left edge, y is the baseline.
	em   float32 // Font size in logical units.
	clr  color.RGBA
}

// colorFor resolves the color of the cluster starting at byteStart. Later
// range overrides replace earlier ones, matching the font range semantics.
func (t *textLayout) colorFor(byteStart int) color.RGBA {
	for i := len(t.colors) - 1; i >= 0; i-- {
		c := t.colors[i]
		if byteStart >= c.Start && byteStart < c.Start+c.Length {
			return c.Color
		}
	}
	return toRGBAColor(t.format.TextColor)
}

// rasterizeGlyphs fills the layout into a premultiplied RGBA bitmap at
// scale physical pixels per logical unit. The backing array of buf is reused
// when its capacity is sufficient.
func (t *textLayout) rasterizeGlyphs(scale float32, buf []byte) (typography.TextBitmap, error) {
	t.relayout()
	bitmap := typography.TextBitmap{}
	// The raster extent is capped by the layout box, as the DirectWrite backend
	// caps it (min(extends, layout size)). Without the cap an unbreakable token
	// makes width grow with the text, so each frame allocates a bigger bitmap and
	// the process dies on OOM rather than reporting a bad size.
	rasterWidth, rasterHeight := t.occupiedWidth, t.occupiedHeight
	if t.maxWidth > 0 {
		rasterWidth = min(rasterWidth, t.maxWidth)
	}
	if t.maxHeight > 0 {
		rasterHeight = min(rasterHeight, t.maxHeight)
	}
	width := int(math.Ceil(float64(rasterWidth * scale)))
	height := int(math.Ceil(float64(rasterHeight * scale)))
	if width <= 0 || height <= 0 {
		return bitmap, nil
	}
	stride := width * 4
	need := stride * height
	var pix []byte
	if cap(buf) >= need {
		pix = buf[:need]
	} else {
		pix = make([]byte, need)
	}
	clear(pix)
	img := &image.RGBA{Pix: pix, Stride: stride, Rect: image.Rect(0, 0, width, height)}
	if t.rasterizer == nil {
		t.rasterizer = vector.NewRasterizer(width, height)
	} else {
		t.rasterizer.Reset(width, height)
	}
	r := t.rasterizer
	for i := range t.rasterGlyphs {
		g := &t.rasterGlyphs[i]
		if g.face == nil {
			continue
		}
		paintGlyph(r, img, g, scale, width, height)
	}
	t.rasterDecorations(img, scale, width, height)
	bitmap.Width, bitmap.Height, bitmap.Stride, bitmap.Pixels = width, height, stride, pix
	return bitmap, nil
}

// paintGlyph fills one glyph outline. The vector rasterizer applies the
// non-zero winding rule, so oppositely wound inner contours cut holes.
func paintGlyph(r *vector.Rasterizer, img *image.RGBA, g *rasterGlyph, scale float32, width, height int) {
	if g.clr.A == 0 {
		return
	}
	upem := float32(g.face.Upem())
	if upem <= 0 || !isFinite32(g.x) || !isFinite32(g.y) {
		return
	}
	outline, ok := g.face.GlyphDataOutline(g.gid)
	if !ok || len(outline.Segments) == 0 {
		return
	}
	k := g.em * scale / upem
	ox := g.x * scale
	oy := g.y * scale
	px := func(fx float32) float32 { return ox + fx*k }
	py := func(fy float32) float32 { return oy - fy*k } // font Y axis points up

	// Cull glyphs entirely outside the bitmap before accumulating the path.
	minX, minY := math.MaxFloat64, math.MaxFloat64
	maxX, maxY := -math.MaxFloat64, -math.MaxFloat64
	for i := range outline.Segments {
		for _, p := range outline.Segments[i].ArgsSlice() {
			x, y := float64(px(p.X)), float64(py(p.Y))
			if x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
			if y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if !(maxX > 0 && maxY > 0 && minX < float64(width) && minY < float64(height)) {
		return
	}

	open := false
	for i := range outline.Segments {
		seg := &outline.Segments[i]
		args := seg.ArgsSlice()
		switch seg.Op {
		case opentype.SegmentOpMoveTo:
			if open {
				r.ClosePath()
			}
			r.MoveTo(px(args[0].X), py(args[0].Y))
			open = true
		case opentype.SegmentOpLineTo:
			r.LineTo(px(args[0].X), py(args[0].Y))
		case opentype.SegmentOpQuadTo:
			r.QuadTo(px(args[0].X), py(args[0].Y), px(args[1].X), py(args[1].Y))
		case opentype.SegmentOpCubeTo:
			r.CubeTo(px(args[0].X), py(args[0].Y), px(args[1].X), py(args[1].Y), px(args[2].X), py(args[2].Y))
		}
	}
	if open {
		r.ClosePath()
	}
	r.Draw(img, img.Bounds(), &image.Uniform{C: g.clr}, image.Point{})
	r.Reset(width, height)
}

// rasterDecorations draws the underline and strikethrough rules requested on
// byte ranges, using the font metrics of each line's first face.
func (t *textLayout) rasterDecorations(img *image.RGBA, scale float32, width, height int) {
	for _, d := range t.decorations {
		if d.Length <= 0 || (!d.Under && !d.Strike) {
			continue
		}
		clr := t.colorFor(d.Start)
		for li := range t.lines {
			x0, x1, found := decorationSpan(&t.lines[li], d)
			if !found {
				continue
			}
			face, em := t.lineFaces[li], t.lineEms[li]
			baseline := t.lines[li].Baseline
			if d.Under {
				fillRule(img, face, em, baseline, x0, x1,
					font.UnderlinePosition, font.UnderlineThickness, -0.1, 0.05, clr, scale, width, height)
			}
			if d.Strike {
				fillRule(img, face, em, baseline, x0, x1,
					font.StrikethroughPosition, font.StrikethroughThickness, 0.3, 0.05, clr, scale, width, height)
			}
		}
	}
}

// decorationSpan returns the horizontal extent of the clusters of one line
// that overlap the decoration byte range.
func decorationSpan(line *typography.TextLine, d decorationRange) (float32, float32, bool) {
	var x0, x1 float32
	found := false
	end := d.Start + d.Length
	for _, c := range line.Clusters {
		if c.Start >= end || c.Start+c.Length <= d.Start {
			continue
		}
		if !found || c.X < x0 {
			x0 = c.X
		}
		if !found || c.X+c.Width > x1 {
			x1 = c.X + c.Width
		}
		found = true
	}
	return x0, x1, found
}

// fillRule paints one horizontal rule with source-over compositing. posM is
// the metric of the rule top above the baseline (negative below) and thickM
// the rule thickness; ratios are the fallback when the font omits them.
func fillRule(img *image.RGBA, face *font.Face, em, baseline, x0, x1 float32,
	posM, thickM font.LineMetric, fallbackPos, fallbackThick float32,
	clr color.RGBA, scale float32, width, height int) {
	if clr.A == 0 || !isFinite32(em) || em <= 0 {
		return
	}
	var pos, thick float32
	if face != nil {
		if upem := float32(face.Upem()); upem > 0 {
			pos = face.LineMetric(posM) / upem * em
			thick = face.LineMetric(thickM) / upem * em
		}
	}
	if pos == 0 || !isFinite32(pos) {
		pos = fallbackPos * em
	}
	if thick <= 0 || !isFinite32(thick) {
		thick = fallbackThick * em
	}
	x0px := max(0, int(math.Floor(float64(x0*scale))))
	x1px := min(width, int(math.Ceil(float64(x1*scale))))
	y0 := int(math.Floor(float64((baseline - pos) * scale)))
	y1 := y0 + max(1, int(math.Ceil(float64(thick*scale))))
	y0 = max(0, y0)
	y1 = min(height, y1)
	rect := image.Rect(x0px, y0, x1px, y1)
	if rect.Empty() {
		return
	}
	draw.Draw(img, rect, &image.Uniform{C: clr}, image.Point{}, draw.Over)
}

func isFinite32(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

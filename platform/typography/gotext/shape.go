package gotext

import (
	"math"
	"sort"
	"unicode/utf8"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"

	"github.com/golang-gui/goui/platform/typography"
)

const (
	// fixedScale converts logical units to the 26.6 fixed point go-text uses.
	fixedScale = 64
	// maxFixedF bounds an unlimited layout axis for the wrapper.
	maxFixedF = fixed.Int26_6(maxInt26_6)
)

const maxInt26_6 = int32(math.MaxInt32)>>1 - 1

// relayout re-shapes and re-wraps the text, then refreshes the cached
// measurement. Coordinates are logical units (DIP); byte offsets are UTF-8.
func (t *textLayout) relayout() {
	if !t.dirty {
		return
	}
	t.dirty = false

	t.lines = t.lines[:0]
	t.clusters = t.clusters[:0]
	t.rasterGlyphs = t.rasterGlyphs[:0]
	t.lineFaces = t.lineFaces[:0]
	t.lineEms = t.lineEms[:0]
	t.occupiedWidth, t.occupiedHeight = 0, 0
	// Called under the context lock. A context that already released its font map
	// reports an empty layout here instead of dereferencing it.
	if t.ctx.destroyed {
		return
	}
	if t.text == "" {
		return
	}

	runes := []rune(t.text)
	byteAt := make([]int, len(runes)+1)
	acc := 0
	for i, r := range runes {
		byteAt[i] = acc
		acc += utf8.RuneLen(r)
	}
	byteAt[len(runes)] = acc

	fonts := t.effectiveFonts(runes, byteAt)

	// Each paragraph continues below the previous one, so the line top is carried
	// across the loop; restarting it per paragraph stacks every line of multi-line
	// text at y=0.
	top := float32(0)
	paraStart := 0
	for i := 0; i <= len(runes); i++ {
		if i < len(runes) && runes[i] != '\n' {
			continue
		}
		top = t.layoutParagraph(runes[paraStart:i], byteAt, fonts, paraStart, i < len(runes), top)
		paraStart = i + 1
	}
	t.occupiedHeight = top
}

// effectiveFonts resolves the per-rune font by applying the range overrides
// in call order, later requests replacing earlier ones.
func (t *textLayout) effectiveFonts(runes []rune, byteAt []int) []typography.FontInfo {
	infos := make([]typography.FontInfo, len(runes))
	for i := range infos {
		infos[i] = t.format.Font
	}
	for _, fr := range t.fonts {
		begin := runeAt(byteAt, fr.Start)
		end := runeAt(byteAt, fr.Start+fr.Length)
		for i := begin; i < end; i++ {
			infos[i] = fr.Font
		}
	}
	return infos
}

// runeAt maps a byte offset to the rune index starting at that offset.
func runeAt(byteAt []int, b int) int {
	return sort.Search(len(byteAt), func(i int) bool { return byteAt[i] >= b })
}

// layoutParagraph shapes one hard-broken paragraph, wraps it into lines and
// appends the line and cluster metrics. para holds the paragraph runes,
// paraStart the rune index of para[0] in the whole text, lineTop the y the first
// line of this paragraph starts at. It returns the y just after the last line.
func (t *textLayout) layoutParagraph(para []rune, byteAt []int, fonts []typography.FontInfo, paraStart int, hasBreak bool, lineTop float32) float32 {
	outputs := t.shapeParagraph(para, byteAt, fonts, paraStart)

	var lines []shaping.Line
	switch t.format.WrapMode {
	case typography.WrapNone:
		lines = []shaping.Line{outputs}
	default:
		cfg := shaping.WrapConfig{Direction: di.DirectionLTR, BreakPolicy: shaping.WhenNecessary}
		if t.format.WrapMode == typography.WrapChar {
			cfg.BreakPolicy = shaping.Always
		}
		width := maxFixedF
		if t.maxWidth > 0 {
			width = fixed.Int26_6(math.Round(float64(t.maxWidth) * fixedScale))
		}
		wrapped, _ := t.wrapper.WrapParagraphF(cfg, width, para, shaping.NewSliceIterator(outputs))
		lines = wrapped
	}

	y := lineTop
	for li, runs := range lines {
		last := li == len(lines)-1
		lineHeight := t.buildLine(para, byteAt, paraStart, runs, y, hasBreak && last)
		y += lineHeight
	}
	return y
}

// shapeParagraph splits the paragraph at style, tab and placeholder
// boundaries, shapes the text runs and splices in synthesized runs for the
// special characters.
func (t *textLayout) shapeParagraph(para []rune, byteAt []int, fonts []typography.FontInfo, paraStart int) shaping.Line {
	var outputs shaping.Line
	// Text ending in a newline gets a final paragraph with no runes of its own.
	// The per-rune font table below is indexed by absolute rune position, so an
	// empty paragraph must stop before fonts[paraStart], which is one past it.
	// The empty line it stands for is still laid out from the face extents.
	if len(para) == 0 {
		return outputs
	}
	ph := t.placeholderRunes(byteAt, paraStart, len(para))

	var lastFace *font.Face
	base := fonts[paraStart]
	size := sizeFixed(base)

	i := 0
	for i < len(para) {
		if para[i] == '\t' {
			outputs = append(outputs, t.tabRun(lastFace, size, i))
			i++
			continue
		}
		if m, ok := ph[i]; ok {
			outputs = append(outputs, placeholderRun(lastFace, size, i, m))
			i++
			continue
		}
		info := fonts[paraStart+i]
		size = sizeFixed(info)
		j := i + 1
		for j < len(para) && para[j] != '\t' {
			if _, ok := ph[j]; ok {
				break
			}
			if fonts[paraStart+j] != info {
				break
			}
			j++
		}
		t.ctx.setFamily(info.Family, fontAspect(info))
		input := shaping.Input{
			Text:      para,
			RunStart:  i,
			RunEnd:    j,
			Direction: di.DirectionLTR,
			Size:      size,
		}
		for _, sub := range t.segmenter.Split(input, faceSelector{fm: t.ctx.fm}) {
			if sub.Face == nil {
				continue
			}
			out := t.shaper.Shape(sub)
			outputs = append(outputs, out)
			lastFace = out.Face
		}
		i = j
	}
	return outputs
}

// placeholderRunes returns the paragraph-relative rune indexes carrying a
// placeholder metric.
func (t *textLayout) placeholderRunes(byteAt []int, paraStart, paraLen int) map[int]typography.PlaceholderMetrics {
	if len(t.placeholders) == 0 {
		return nil
	}
	out := make(map[int]typography.PlaceholderMetrics)
	for b, m := range t.placeholders {
		r := runeAt(byteAt, b)
		if r >= paraStart && r < paraStart+paraLen && byteAt[r] == b {
			out[r-paraStart] = m
		}
	}
	return out
}

func sizeFixed(info typography.FontInfo) fixed.Int26_6 {
	return fixed.Int26_6(math.Round(float64(ptToDip(info.Size)) * fixedScale))
}

func (t *textLayout) baseFace() *font.Face {
	info := t.format.Font
	t.ctx.setFamily(info.Family, fontAspect(info))
	return t.ctx.fm.ResolveFace(' ')
}

// tabRun synthesizes the zero-width shaping result standing for a tab; the
// line walk replaces its advance with the distance to the next tab stop.
func (t *textLayout) tabRun(face *font.Face, size fixed.Int26_6, runeIdx int) shaping.Output {
	if face == nil {
		face = t.baseFace()
	}
	out := shaping.Output{
		Face:      face,
		Size:      size,
		Runes:     shaping.Range{Offset: runeIdx, Count: 1},
		Direction: di.DirectionLTR,
	}
	out.Glyphs = []shaping.Glyph{{
		GlyphID:      font.EmptyGlyph,
		ClusterIndex: 0,
		RuneCount:    1,
		GlyphCount:   1,
	}}
	return out
}

// placeholderRun reserves the metrics rectangle in the line, matching how the
// DirectWrite backend handles placeholder ranges.
func placeholderRun(face *font.Face, size fixed.Int26_6, runeIdx int, m typography.PlaceholderMetrics) shaping.Output {
	out := shaping.Output{
		Face:      face,
		Size:      size,
		Runes:     shaping.Range{Offset: runeIdx, Count: 1},
		Direction: di.DirectionLTR,
		LineBounds: shaping.Bounds{
			Ascent:  fixed.Int26_6(math.Round(float64(m.Baseline) * fixedScale)),
			Descent: -fixed.Int26_6(math.Round(float64(m.Height-m.Baseline) * fixedScale)),
		},
	}
	out.Glyphs = []shaping.Glyph{{
		GlyphID:      font.EmptyGlyph,
		ClusterIndex: 0,
		RuneCount:    1,
		GlyphCount:   1,
		Advance:      fixed.Int26_6(math.Round(float64(m.Width) * fixedScale)),
	}}
	return out
}

// buildLine turns one wrapped line of runs into line and cluster metrics and
// returns the line height.
func (t *textLayout) buildLine(para []rune, byteAt []int, paraStart int, runs shaping.Line, lineTop float32, hasBreak bool) float32 {
	lineIndex := len(t.lines)

	// Line vertical extent from the run bounds.
	var ascent, descent, gap float32
	for _, r := range runs {
		if f := f32(r.LineBounds.Ascent); f > ascent {
			ascent = f
		}
		if f := f32(r.LineBounds.Descent); f < descent {
			descent = f
		}
		if f := f32(r.LineBounds.Gap); f > gap {
			gap = f
		}
	}
	if len(runs) == 0 {
		if face := t.baseFace(); face != nil {
			if ext, ok := face.FontHExtents(); ok {
				em := ptToDip(t.format.Font.Size)
				upem := float32(face.Upem())
				ascent = ext.Ascender / upem * em
				descent = ext.Descender / upem * em
				gap = ext.LineGap / upem * em
			}
		}
	}
	lineHeight := ascent - descent + gap
	baseline := lineTop + ascent

	// Horizontal pen walk in logical order, resolving tab stops and
	// collecting the cluster metrics.
	clustersBeg := len(t.clusters)
	rasterBeg := len(t.rasterGlyphs)
	firstRune, lastRune := -1, -1
	pen := float32(0)
	for _, r := range runs {
		direction := typography.TextLeftToRight
		if r.Direction.Progression() == di.TowardTopLeft {
			direction = typography.TextRightToLeft
		}
		if begin := r.Runes.Offset; firstRune < 0 || begin < firstRune {
			firstRune = begin
		}
		if end := r.Runes.Offset + r.Runes.Count; end > lastRune {
			lastRune = end
		}
		for gi := 0; gi < len(r.Glyphs); {
			g := r.Glyphs[gi]
			ci := g.ClusterIndex
			// TextIndex is already absolute in the paragraph text, matching
			// the RunStart passed to the shaper.
			clusterRune := g.TextIndex()
			width := float32(0)
			n := gi
			for n < len(r.Glyphs) && r.Glyphs[n].ClusterIndex == ci {
				width += f32(r.Glyphs[n].Advance)
				n++
			}
			start := pen
			widthApplied := false
			if clusterRune >= 0 && clusterRune < len(para) && para[clusterRune] == '\t' {
				pen = nextTabStop(pen, t.tabStop())
				width = pen - start
				widthApplied = true
			}
			cluster := typography.TextCluster{
				X:         start,
				Y:         lineTop,
				Width:     width,
				Height:    lineHeight,
				LineIndex: lineIndex,
				Direction: direction,
			}
			if clusterRune >= 0 && clusterRune < len(para) {
				global := paraStart + clusterRune
				cluster.Start = byteAt[global]
				cluster.Length = byteAt[global+g.RunesCount()] - cluster.Start
			}
			if m, ok := t.placeholders[cluster.Start]; ok {
				cluster.Y = baseline - m.Baseline
				cluster.Height = m.Height
			}
			// Collect visible glyphs for the raster pass, positioned inside
			// the cluster advance.
			clr := t.colorFor(cluster.Start)
			acc := float32(0)
			for k := gi; k < n; k++ {
				gn := r.Glyphs[k]
				if gn.GlyphID != font.EmptyGlyph && r.Face != nil {
					t.rasterGlyphs = append(t.rasterGlyphs, rasterGlyph{
						face: r.Face,
						gid:  gn.GlyphID,
						x:    start + acc + f32(gn.XOffset),
						y:    baseline - f32(gn.YOffset),
						em:   f32(r.Size),
						clr:  clr,
					})
				}
				acc += f32(gn.Advance)
			}
			if !widthApplied {
				pen = start + width
			}
			t.clusters = append(t.clusters, cluster)
			gi = n
		}
	}
	if firstRune < 0 {
		firstRune, lastRune = 0, 0
	}

	width := pen
	alignX := float32(0)
	switch t.format.TextAlign {
	case typography.TextAlignCenter:
		if t.maxWidth > width {
			alignX = (t.maxWidth - width) / 2
		}
	case typography.TextAlignEnd:
		if t.maxWidth > width {
			alignX = t.maxWidth - width
		}
	}
	// TextAlignFill is treated like Begin in this backend.

	for i := clustersBeg; i < len(t.clusters); i++ {
		t.clusters[i].X += alignX
	}
	for i := rasterBeg; i < len(t.rasterGlyphs); i++ {
		t.rasterGlyphs[i].x += alignX
	}
	var face *font.Face
	var em float32
	for _, r := range runs {
		if r.Face != nil {
			face, em = r.Face, f32(r.Size)
			break
		}
	}
	if face == nil {
		face, em = t.baseFace(), ptToDip(t.format.Font.Size)
	}
	t.lineFaces = append(t.lineFaces, face)
	t.lineEms = append(t.lineEms, em)

	start := byteAt[paraStart]
	length := 0
	if firstRune >= 0 {
		start = byteAt[paraStart+firstRune]
		length = byteAt[paraStart+lastRune] - start
	}
	if hasBreak {
		length++ // the hard line break counts into the paragraph's last line
	}
	t.lines = append(t.lines, typography.TextLine{
		Start:    start,
		Length:   length,
		X:        alignX,
		Y:        lineTop,
		Width:    width,
		Height:   lineHeight,
		Baseline: baseline,
		Clusters: t.clusters[clustersBeg:],
	})
	if width > t.occupiedWidth {
		t.occupiedWidth = width
	}
	return lineHeight
}

// f32 converts a 26.6 fixed point value to logical units.
func f32(v fixed.Int26_6) float32 {
	return float32(v) / fixedScale
}

// nextTabStop advances x to the next multiple of width, the incremental tab
// stop semantics the DirectWrite backend implements.
func nextTabStop(x, width float32) float32 {
	if width <= 0 {
		return x
	}
	return (float32(math.Floor(float64(x)/float64(width))) + 1) * width
}

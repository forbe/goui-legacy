package gotext

import (
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
	"github.com/go-text/typesetting/language"

	"github.com/golang-gui/goui/platform/typography"
)

// fontAspect maps the backend-neutral font description onto go-text's
// matching aspect. Unknown values keep go-text's defaults.
func fontAspect(info typography.FontInfo) font.Aspect {
	a := font.Aspect{Weight: font.Weight(info.Weight)}
	switch info.Style {
	case typography.FontStyleItalic, typography.FontStyleOblique:
		a.Style = font.StyleItalic
	default:
		a.Style = font.StyleNormal
	}
	switch info.Width {
	case typography.FontWidthUltraCondensed:
		a.Stretch = font.StretchUltraCondensed
	case typography.FontWidthExtraCondensed:
		a.Stretch = font.StretchExtraCondensed
	case typography.FontWidthSemiCondensed:
		a.Stretch = font.StretchSemiCondensed
	case typography.FontWidthCondensed:
		a.Stretch = font.StretchCondensed
	case typography.FontWidthSemiExpanded:
		a.Stretch = font.StretchSemiExpanded
	case typography.FontWidthExpanded:
		a.Stretch = font.StretchExpanded
	case typography.FontWidthExtraExpanded:
		a.Stretch = font.StretchExtraExpanded
	case typography.FontWidthUltraExpanded:
		a.Stretch = font.StretchUltraExpanded
	default:
		a.Stretch = font.StretchNormal
	}
	return a
}

// faceSelector adapts the shared FontMap to shaping.Fontmap. The caller sets
// the family query before use; per-script selection and rune coverage
// fallback stay inside fontscan.
type faceSelector struct {
	fm *fontscan.FontMap
}

func (s faceSelector) SetScript(sc language.Script) { s.fm.SetScript(sc) }

func (s faceSelector) ResolveFace(r rune) *font.Face { return s.fm.ResolveFace(r) }

// setFamily points the map at the requested family. An unknown family is not
// an error: ResolveFace then walks the fallback chain, matching the DirectWrite
// contract that the selected face may be a fallback.
func (c *context) setFamily(family string, aspect font.Aspect) {
	c.fm.SetQuery(fontscan.Query{
		Families: []string{family},
		Aspect:   aspect,
	})
}

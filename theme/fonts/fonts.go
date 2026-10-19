// Package fonts holds the font files shipped with this library as bytes, for
// in-memory registration through gui.Application.AddFontData or ui.App.AddFontData.
//
// A consumer cannot go:embed across a module boundary, so the bytes are exported from
// the package that owns the files. Request the family by the name each value documents;
// on a system without in-memory font support (DirectWrite older than 1.3, Windows 7
// included) that name still resolves to a fallback face rather than to these bytes.
package fonts

import _ "embed"

// AlibabaPuHuiTi is the regular weight of Alibaba PuHuiTi, freeware for commercial
// use. Its DirectWrite family name is "Alibaba PuHuiTi 2.0".
//
//go:embed alibaba_puhui.ttf
var AlibabaPuHuiTi []byte

// IconFont is an icon glyph font whose DirectWrite family name is "iconfont"; the
// code points are symbols, not text.
//
//go:embed iconfont.new.ttf
var IconFont []byte

// Package gotext implements the typography.Context contract in pure Go,
// based on github.com/go-text/typesetting. It is the fallback backend for
// systems without DirectWrite (for example Windows Server 2008 without the
// platform update), and carries no native library dependency.
package gotext

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-text/typesetting/fontscan"

	"github.com/golang-gui/goui/platform/typography"
	"github.com/golang-gui/goui/platform/typography/utils"
)

type context struct {
	// mu serializes every use of the font map. go-text documents FontMap and the
	// face caches it hands out as not concurrent-safe, and one resolved face is
	// shared by every layout of this context, so shaping and font registration
	// cannot overlap. Typography stays GUI-thread bound by contract; this makes an
	// off-thread call cost contention instead of a fatal concurrent map write.
	mu        sync.Mutex
	destroyed bool

	// fm indexes every face available to layouts created by this context.
	// go-text faces are plain Go objects; the garbage collector reclaims them
	// once the map is dropped, so there is no native release order to track.
	fm *fontscan.FontMap
	// fontFiles records registered file paths so AddFont is idempotent.
	fontFiles map[string]bool
	// fontData holds the bytes already registered through AddFontData, keyed by
	// their hash so a rebuild that re-sends the same font costs nothing.
	fontData map[[sha256.Size]byte]bool
}

var (
	_ typography.Context       = (*context)(nil)
	_ typography.FontDataAdder = (*context)(nil)
)

// errDestroyedContext reports a released font map. It is an error rather than a
// panic because the platform can be destroyed while a widget still holds text.
var errDestroyedContext = errors.New("gotext: typography context is destroyed")

// NewContext creates the pure-Go typography backend. System font discovery
// is best-effort: a failed scan leaves the context usable for fonts registered
// through AddFont or AddFontData.
func NewContext() (typography.Context, error) {
	c := &context{fm: fontscan.NewFontMap(nil)}
	cacheDir := ""
	if base, err := os.UserCacheDir(); err == nil {
		dir := filepath.Join(base, "goui", "gotext")
		if err := os.MkdirAll(dir, 0o700); err == nil {
			cacheDir = dir
		}
	}
	// A missing or partial font directory on an old machine must not fail the
	// context; layouts then rely on application-registered fonts.
	if err := c.fm.UseSystemFonts(cacheDir); err != nil {
		log.Printf("gotext: system fonts unavailable (%v); using registered fonts only", err)
	}
	return c, nil
}

func (c *context) Name() string {
	return "GoText"
}

func (c *context) Destroy() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.destroyed = true
	c.fm = nil
	c.fontFiles = nil
	c.fontData = nil
}

func (c *context) AddFont(fontFile string) error {
	path, err := filepath.Abs(fontFile)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.destroyed {
		return errDestroyedContext
	}
	if c.fontFiles[path] {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := c.fm.AddFont(file, path, ""); err != nil {
		return err
	}
	if c.fontFiles == nil {
		c.fontFiles = make(map[string]bool)
	}
	c.fontFiles[path] = true
	return nil
}

// AddFontData registers the faces held in data, so an application needs no
// font file on disk. Registration is idempotent by content hash.
func (c *context) AddFontData(data []byte) error {
	if len(data) == 0 {
		return errors.New("gotext: empty font data")
	}
	key := sha256.Sum256(data)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.destroyed {
		return errDestroyedContext
	}
	if c.fontData[key] {
		return nil
	}
	fileID := fmt.Sprintf("memory:%x", key[:8])
	if err := c.fm.AddFont(bytes.NewReader(data), fileID, ""); err != nil {
		return err
	}
	if c.fontData == nil {
		c.fontData = make(map[[sha256.Size]byte]bool)
	}
	c.fontData[key] = true
	return nil
}

func (c *context) NewTextLayout(text string, format typography.TextFormat, width, height float32) (typography.TextLayout, error) {
	if err := utils.ValidateTabWidth(format.TabWidth); err != nil {
		return nil, err
	}
	if _, err := utils.ResolveFont(format.Font); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.destroyed {
		return nil, errDestroyedContext
	}
	// ResolveFace only returns nil when the font database is empty; without any
	// face the backend cannot shape anything.
	if c.fm.ResolveFace(' ') == nil {
		return nil, errors.New("gotext: no fonts available")
	}
	// Safe to build under the lock: the constructor only records the format and
	// marks the layout dirty, so the first measurement takes the lock itself.
	return newTextLayout(c, text, format, width, height), nil
}

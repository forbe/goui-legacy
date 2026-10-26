package directwrite

import (
	"runtime"
	"testing"

	"github.com/golang-gui/goui/platform/typography"
)

func TestProbeOffThreadRaster(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx, err := NewContext()
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	defer ctx.Destroy()
	done := make(chan error, 1)
	go func() {
		layout, err := ctx.NewTextLayout("hello world", typography.TextFormat{
			Font: typography.FontInfo{Family: "Segoe UI", Size: 12},
		}, 200, 40)
		if err != nil {
			done <- err
			return
		}
		defer layout.Destroy()
		_, err = layout.Rasterize(1, nil)
		done <- err
	}()
	if err := <-done; err != nil {
		t.Fatalf("off-thread rasterize: %v", err)
	}
}

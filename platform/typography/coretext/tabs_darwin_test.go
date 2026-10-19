package coretext

import (
	"runtime"
	"testing"

	"github.com/forbe/goui-legacy/platform/typography/internal/typographytest"
)

func TestTabWidth(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx, err := NewContext()
	if err != nil {
		t.Fatal(err)
	}
	defer ctx.Destroy()
	typographytest.Tabs(t, ctx)
}

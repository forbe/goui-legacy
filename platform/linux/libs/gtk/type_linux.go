package gtk

import "github.com/forbe/goui-legacy/platform/linux/libs/glib"

type Settings struct {
	glib.Object
}

type StyleContext struct {
	glib.Object
}

type RGBA struct {
	Red   float64
	Green float64
	Blue  float64
	Alpha float64
}

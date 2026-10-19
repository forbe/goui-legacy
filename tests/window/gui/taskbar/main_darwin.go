package main

import "github.com/forbe/goui-legacy/gui"

// AppKit has no per-window taskbar entry. Dock policy and native interaction
// require desktop observation; do not pretend a cached option is a native fact.
func checkNative(gui.Window, bool) error { return nil }

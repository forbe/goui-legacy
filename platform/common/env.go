package common

import (
	"os"
	"strconv"
)

func GetPreferPainter() string {
	return os.Getenv("GOUI_PLAT_PAINTER")
}

// GetPreferTypography returns the GOUI_PLAT_TYPOGRAPHY env override. "gotext"
// forces the pure-Go typography backend even when DirectWrite is available.
func GetPreferTypography() string {
	return os.Getenv("GOUI_PLAT_TYPOGRAPHY")
}

// GetPreferScale returns the GOUI_PLAT_SCALE env override. Returns 0 if unset
// or invalid, in which case the platform's auto-detected scale is used.
func GetPreferScale() float32 {
	if v, err := strconv.ParseFloat(os.Getenv("GOUI_PLAT_SCALE"), 32); err == nil && v > 0 {
		return float32(v)
	}
	return 0
}

package platform

import "github.com/forbe/goui-legacy/platform/linux/x11"

func newPlatform(name, appId string) (Platform, error) {
	if name != "x11" {
		return nil, ErrUnsupported
	}
	p, err := x11.NewPlatform(appId)
	if err != nil {
		return nil, err
	}
	return p, nil
}

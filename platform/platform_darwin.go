package platform

import "github.com/forbe/goui-legacy/platform/darwin/cocoa"

func newPlatform(name, appId string) (Platform, error) {
	if name != "cocoa" {
		return nil, ErrUnsupported
	}
	p, err := cocoa.NewPlatform(appId)
	if err != nil {
		return nil, err
	}
	return p, nil
}

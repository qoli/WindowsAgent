//go:build !windows || !amd64

package scriptrunner

import "errors"

type unsupportedNativeBackend struct{}

func newNativeBackend() nativeBackend {
	return unsupportedNativeBackend{}
}

func (unsupportedNativeBackend) load(string) (nativeDLL, error) {
	return nil, errors.New("native libraries require Windows amd64")
}

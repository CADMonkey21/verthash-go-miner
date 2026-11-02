//go:build !windows

package cuda

import "github.com/ebitengine/purego"

func openNVMLLibrary() (uintptr, error) {
	if h, err := purego.Dlopen("libnvidia-ml.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
		return h, nil
	}
	return purego.Dlopen("libnvidia-ml.so", purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

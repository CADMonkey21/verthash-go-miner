//go:build linux

package cuda

import "github.com/ebitengine/purego"

func openCUDA() (uintptr, error) {
	return purego.Dlopen("libcuda.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

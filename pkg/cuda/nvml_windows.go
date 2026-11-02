//go:build windows

package cuda

import (
	"os"
	"path/filepath"
	"syscall"
)

func openNVMLLibrary() (uintptr, error) {
	if h, err := syscall.LoadLibrary("nvml.dll"); err == nil {
		return uintptr(h), nil
	}
	programFiles := os.Getenv("ProgramFiles")
	if programFiles == "" {
		programFiles = `C:\Program Files`
	}
	nvsmiPath := filepath.Join(programFiles, "NVIDIA Corporation", "NVSMI", "nvml.dll")
	h, err := syscall.LoadLibrary(nvsmiPath)
	if err != nil {
		return 0, err
	}
	return uintptr(h), nil
}

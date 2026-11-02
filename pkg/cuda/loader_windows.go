//go:build windows

package cuda

import "syscall"

func openCUDA() (uintptr, error) {
	handle, err := syscall.LoadLibrary("nvcuda.dll")
	return uintptr(handle), err
}

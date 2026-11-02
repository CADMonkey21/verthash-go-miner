//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"syscall"

	"github.com/ebitengine/purego"
)

var (
	nvrtcCreateProgram     func(prog *uintptr, src *byte, name *byte, numHeaders int32, headers *uintptr, includeNames *uintptr) uint32
	nvrtcCompileProgram    func(prog uintptr, numOptions int32, options **byte) uint32
	nvrtcGetProgramLogSize func(prog uintptr, logSizeRet *uintptr) uint32
	nvrtcGetProgramLog     func(prog uintptr, logBuf *byte) uint32
	nvrtcGetPTXSize        func(prog uintptr, ptxSizeRet *uintptr) uint32
	nvrtcGetPTX            func(prog uintptr, ptx *byte) uint32
)

func main() {
	cudaBin := `C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v12.6\bin`
	_ = os.Setenv("PATH", cudaBin+";"+os.Getenv("PATH"))

	h, err := syscall.LoadLibrary(cudaBin + `\nvrtc64_120_0.dll`)
	if err != nil {
		log.Fatalf("Failed to load nvrtc64_120_0.dll: %v", err)
	}
	lib := uintptr(h)

	purego.RegisterLibFunc(&nvrtcCreateProgram, lib, "nvrtcCreateProgram")
	purego.RegisterLibFunc(&nvrtcCompileProgram, lib, "nvrtcCompileProgram")
	purego.RegisterLibFunc(&nvrtcGetProgramLogSize, lib, "nvrtcGetProgramLogSize")
	purego.RegisterLibFunc(&nvrtcGetProgramLog, lib, "nvrtcGetProgramLog")
	purego.RegisterLibFunc(&nvrtcGetPTXSize, lib, "nvrtcGetPTXSize")
	purego.RegisterLibFunc(&nvrtcGetPTX, lib, "nvrtcGetPTX")

	srcBytes, err := os.ReadFile("pkg/cuda/verthash.cu")
	if err != nil {
		log.Fatalf("Failed to read pkg/cuda/verthash.cu: %v", err)
	}
	srcNull := append(srcBytes, 0)
	nameNull := append([]byte("verthash.cu"), 0)

	var prog uintptr
	if res := nvrtcCreateProgram(&prog, &srcNull[0], &nameNull[0], 0, nil, nil); res != 0 {
		log.Fatalf("nvrtcCreateProgram failed: %d", res)
	}

	optArch := append([]byte("--gpu-architecture=compute_75"), 0)
	opts := []*byte{&optArch[0]}

	res := nvrtcCompileProgram(prog, 1, &opts[0])

	var logSize uintptr
	nvrtcGetProgramLogSize(prog, &logSize)
	if logSize > 1 {
		logBuf := make([]byte, logSize)
		nvrtcGetProgramLog(prog, &logBuf[0])
		fmt.Printf("NVRTC Log:\n%s\n", string(logBuf))
	}
	if res != 0 {
		log.Fatalf("nvrtcCompileProgram failed with code %d", res)
	}

	var ptxSize uintptr
	nvrtcGetPTXSize(prog, &ptxSize)
	ptxBuf := make([]byte, ptxSize)
	nvrtcGetPTX(prog, &ptxBuf[0])

	// Trim trailing null byte before writing .ptx file
	if len(ptxBuf) > 0 && ptxBuf[len(ptxBuf)-1] == 0 {
		ptxBuf = ptxBuf[:len(ptxBuf)-1]
	}

	if err := os.WriteFile("pkg/cuda/verthash.ptx", ptxBuf, 0644); err != nil {
		log.Fatalf("Failed to write pkg/cuda/verthash.ptx: %v", err)
	}
	fmt.Printf("Successfully generated pkg/cuda/verthash.ptx (%d bytes) via zero-CGO NVRTC!\n", len(ptxBuf))
}

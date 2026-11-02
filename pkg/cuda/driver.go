package cuda

import (
	"bytes"
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

type (
	CUresult    uint32
	CUdevice    int32
	CUcontext   uintptr
	CUmodule    uintptr
	CUfunction  uintptr
	CUdeviceptr uintptr
	CUstream    uintptr
)

const (
	CUDA_SUCCESS               CUresult = 0
	CU_CTX_SCHED_BLOCKING_SYNC uint32   = 0x04
	CU_MEMHOSTALLOC_DEVICEMAP  uint32   = 0x02
)

var (
	CuInit                    func(flags uint32) CUresult
	CuDriverGetVersion        func(driverVersion *int32) CUresult
	CuDeviceGetCount          func(count *int32) CUresult
	CuDeviceGet               func(device *CUdevice, ordinal int32) CUresult
	CuDeviceGetName           func(name *byte, len int32, dev CUdevice) CUresult
	CuDeviceTotalMem          func(bytes *uintptr, dev CUdevice) CUresult
	CuCtxCreate               func(pctx *CUcontext, flags uint32, dev CUdevice) CUresult
	CuModuleLoadData          func(module *CUmodule, image unsafe.Pointer) CUresult
	CuModuleGetFunction       func(hfunc *CUfunction, hmod CUmodule, name *byte) CUresult
	CuMemAlloc                func(dptr *CUdeviceptr, bytesize uintptr) CUresult
	CuMemHostAlloc            func(pp *unsafe.Pointer, bytesize uintptr, flags uint32) CUresult
	CuMemHostGetDevicePointer func(pdptr *CUdeviceptr, p unsafe.Pointer, flags uint32) CUresult
	CuMemcpyHtoD              func(dstDevice CUdeviceptr, srcHost unsafe.Pointer, byteCount uintptr) CUresult
	CuLaunchKernel            func(f CUfunction, gridDimX, gridDimY, gridDimZ, blockDimX, blockDimY, blockDimZ, sharedMemBytes uint32, hStream CUstream, kernelParams *unsafe.Pointer, extra *unsafe.Pointer) CUresult
	CuStreamSynchronize       func(hStream CUstream) CUresult
)

type DeviceInfo struct {
	Index   int32
	Name    string
	TotalMB uint64
}

func InitDriver() (int32, []DeviceInfo, error) {
	lib, err := openCUDA()
	if err != nil {
		return 0, nil, fmt.Errorf("failed to load CUDA driver library: %w", err)
	}

	purego.RegisterLibFunc(&CuInit, lib, "cuInit")
	purego.RegisterLibFunc(&CuDriverGetVersion, lib, "cuDriverGetVersion")
	purego.RegisterLibFunc(&CuDeviceGetCount, lib, "cuDeviceGetCount")
	purego.RegisterLibFunc(&CuDeviceGet, lib, "cuDeviceGet")
	purego.RegisterLibFunc(&CuDeviceGetName, lib, "cuDeviceGetName")
	purego.RegisterLibFunc(&CuDeviceTotalMem, lib, "cuDeviceTotalMem_v2")
	purego.RegisterLibFunc(&CuCtxCreate, lib, "cuCtxCreate_v2")
	purego.RegisterLibFunc(&CuModuleLoadData, lib, "cuModuleLoadData")
	purego.RegisterLibFunc(&CuModuleGetFunction, lib, "cuModuleGetFunction")
	purego.RegisterLibFunc(&CuMemAlloc, lib, "cuMemAlloc_v2")
	purego.RegisterLibFunc(&CuMemHostAlloc, lib, "cuMemHostAlloc")
	purego.RegisterLibFunc(&CuMemHostGetDevicePointer, lib, "cuMemHostGetDevicePointer_v2")
	purego.RegisterLibFunc(&CuMemcpyHtoD, lib, "cuMemcpyHtoD_v2")
	purego.RegisterLibFunc(&CuLaunchKernel, lib, "cuLaunchKernel")
	purego.RegisterLibFunc(&CuStreamSynchronize, lib, "cuStreamSynchronize")

	if res := CuInit(0); res != CUDA_SUCCESS {
		return 0, nil, fmt.Errorf("cuInit failed with code %d", res)
	}

	var driverVer int32
	CuDriverGetVersion(&driverVer)

	var count int32
	if res := CuDeviceGetCount(&count); res != CUDA_SUCCESS {
		return driverVer, nil, fmt.Errorf("cuDeviceGetCount failed with code %d", res)
	}

	var devices []DeviceInfo
	for i := int32(0); i < count; i++ {
		var dev CUdevice
		CuDeviceGet(&dev, i)

		nameBuf := make([]byte, 256)
		CuDeviceGetName(&nameBuf[0], 256, dev)
		name := string(bytes.TrimRight(nameBuf, "\x00"))

		var totalBytes uintptr
		CuDeviceTotalMem(&totalBytes, dev)

		devices = append(devices, DeviceInfo{
			Index:   i,
			Name:    name,
			TotalMB: uint64(totalBytes) / (1024 * 1024),
		})
	}

	return driverVer, devices, nil
}

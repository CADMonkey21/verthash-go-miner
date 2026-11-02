package cuda

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"runtime"
	"unsafe"
)

//go:embed verthash.ptx
var verthashPTX []byte

type Engine struct {
	DeviceID    int32
	ctx         CUcontext
	mod         CUmodule
	fnMidstate  CUfunction
	fnHashOne   CUfunction
	fnSearch    CUfunction
	dDat        CUdeviceptr
	dHdr        CUdeviceptr
	dMidstates  CUdeviceptr
	dOutHash    CUdeviceptr
	dResults    CUdeviceptr
	dAbort      CUdeviceptr
	pHeader     unsafe.Pointer
	pOutHash    unsafe.Pointer
	pResults    unsafe.Pointer
	pAbort      unsafe.Pointer
	hostHeader  []byte
	hostOutHash []byte
	hostResults []uint32
	hostAbort   *uint32
	mdiv        uint32
}

func NewEngine(deviceID int32, verthashDat []byte) (*Engine, error) {
	runtime.LockOSThread()

	var dev CUdevice
	if res := CuDeviceGet(&dev, deviceID); res != CUDA_SUCCESS {
		return nil, fmt.Errorf("cuDeviceGet(%d) failed: %d", deviceID, res)
	}

	var ctx CUcontext
	if res := CuCtxCreate(&ctx, CU_CTX_SCHED_BLOCKING_SYNC, dev); res != CUDA_SUCCESS {
		return nil, fmt.Errorf("cuCtxCreate(%d) failed: %d", deviceID, res)
	}

	var mod CUmodule
	ptxNull := append(verthashPTX, 0)
	if res := CuModuleLoadData(&mod, unsafe.Pointer(&ptxNull[0])); res != CUDA_SUCCESS {
		return nil, fmt.Errorf("cuModuleLoadData failed: %d", res)
	}

	var fnMidstate, fnHashOne, fnSearch CUfunction
	nameMid := append([]byte("verthash_precompute_midstates"), 0)
	if res := CuModuleGetFunction(&fnMidstate, mod, &nameMid[0]); res != CUDA_SUCCESS {
		return nil, fmt.Errorf("cuModuleGetFunction(verthash_precompute_midstates) failed: %d", res)
	}
	nameOne := append([]byte("verthash_hash_one"), 0)
	if res := CuModuleGetFunction(&fnHashOne, mod, &nameOne[0]); res != CUDA_SUCCESS {
		return nil, fmt.Errorf("cuModuleGetFunction(verthash_hash_one) failed: %d", res)
	}
	nameSearch := append([]byte("verthash_search"), 0)
	if res := CuModuleGetFunction(&fnSearch, mod, &nameSearch[0]); res != CUDA_SUCCESS {
		return nil, fmt.Errorf("cuModuleGetFunction(verthash_search) failed: %d", res)
	}

	var dDat, dHdr, dMidstates, dOutHash, dResults, dAbort CUdeviceptr
	if res := CuMemAlloc(&dDat, uintptr(len(verthashDat))); res != CUDA_SUCCESS {
		return nil, fmt.Errorf("cuMemAlloc(verthash.dat) failed: %d", res)
	}
	if res := CuMemcpyHtoD(dDat, unsafe.Pointer(&verthashDat[0]), uintptr(len(verthashDat))); res != CUDA_SUCCESS {
		return nil, fmt.Errorf("cuMemcpyHtoD(verthash.dat) failed: %d", res)
	}

	CuMemAlloc(&dHdr, 80)
	CuMemAlloc(&dMidstates, 8*25*8) // 8 iterations x 25 uint64s (1,600 bytes)

	var pHeader, pOutHash, pResults, pAbort unsafe.Pointer
	CuMemHostAlloc(&pHeader, 80, 0)
	CuMemHostAlloc(&pOutHash, 32, CU_MEMHOSTALLOC_DEVICEMAP)
	CuMemHostAlloc(&pResults, 64, CU_MEMHOSTALLOC_DEVICEMAP)
	CuMemHostAlloc(&pAbort, 4, CU_MEMHOSTALLOC_DEVICEMAP)

	CuMemHostGetDevicePointer(&dOutHash, pOutHash, 0)
	CuMemHostGetDevicePointer(&dResults, pResults, 0)
	CuMemHostGetDevicePointer(&dAbort, pAbort, 0)

	mdiv := ((uint32(len(verthashDat)) - 32) / 16) + 1

	return &Engine{
		DeviceID:    deviceID,
		ctx:         ctx,
		mod:         mod,
		fnMidstate:  fnMidstate,
		fnHashOne:   fnHashOne,
		fnSearch:    fnSearch,
		dDat:        dDat,
		dHdr:        dHdr,
		dMidstates:  dMidstates,
		dOutHash:    dOutHash,
		dResults:    dResults,
		dAbort:      dAbort,
		pHeader:     pHeader,
		pOutHash:    pOutHash,
		pResults:    pResults,
		pAbort:      pAbort,
		hostHeader:  unsafe.Slice((*byte)(pHeader), 80),
		hostOutHash: unsafe.Slice((*byte)(pOutHash), 32),
		hostResults: unsafe.Slice((*uint32)(pResults), 16),
		hostAbort:   (*uint32)(pAbort),
		mdiv:        mdiv,
	}, nil
}

func (e *Engine) SetHeader(header80 [80]byte) error {
	copy(e.hostHeader, header80[:])
	CuMemcpyHtoD(e.dHdr, e.pHeader, 80)
	*e.hostAbort = 0

	args := [...]unsafe.Pointer{
		unsafe.Pointer(&e.dHdr),
		unsafe.Pointer(&e.dMidstates),
	}
	if res := CuLaunchKernel(e.fnMidstate, 1, 1, 1, 8, 1, 1, 0, 0, &args[0], nil); res != CUDA_SUCCESS {
		return fmt.Errorf("cuLaunchKernel(verthash_precompute_midstates) failed: %d", res)
	}
	return nil
}

func (e *Engine) HashOne(header80 [80]byte) ([32]byte, error) {
	if err := e.SetHeader(header80); err != nil {
		return [32]byte{}, err
	}

	args := [...]unsafe.Pointer{
		unsafe.Pointer(&e.dDat),
		unsafe.Pointer(&e.mdiv),
		unsafe.Pointer(&e.dHdr),
		unsafe.Pointer(&e.dMidstates),
		unsafe.Pointer(&e.dOutHash),
	}

	if res := CuLaunchKernel(e.fnHashOne, 1, 1, 1, 1, 1, 1, 0, 0, &args[0], nil); res != CUDA_SUCCESS {
		return [32]byte{}, fmt.Errorf("cuLaunchKernel(verthash_hash_one) failed: %d", res)
	}
	CuStreamSynchronize(0)

	var out [32]byte
	copy(out[:], e.hostOutHash)
	return out, nil
}

func (e *Engine) SearchBatch(startNonce uint32, gridBlocks, threadsPerBlock uint32, targetU64 uint64) ([]uint32, error) {
	e.hostResults[0] = 0

	args := [...]unsafe.Pointer{
		unsafe.Pointer(&e.dDat),
		unsafe.Pointer(&e.mdiv),
		unsafe.Pointer(&e.dHdr),
		unsafe.Pointer(&e.dMidstates),
		unsafe.Pointer(&startNonce),
		unsafe.Pointer(&targetU64),
		unsafe.Pointer(&e.dResults),
		unsafe.Pointer(&e.dAbort),
	}

	if res := CuLaunchKernel(e.fnSearch, gridBlocks, 1, 1, threadsPerBlock, 1, 1, 0, 0, &args[0], nil); res != CUDA_SUCCESS {
		return nil, fmt.Errorf("cuLaunchKernel(verthash_search) failed: %d", res)
	}
	CuStreamSynchronize(0)

	count := e.hostResults[0]
	if count == 0 {
		return nil, nil
	}
	if count > 15 {
		count = 15
	}
	found := make([]uint32, count)
	copy(found, e.hostResults[1:1+count])
	return found, nil
}

func (e *Engine) Abort() {
	*e.hostAbort = 1
}

func PutNonce(hdr *[80]byte, nonce uint32) {
	binary.LittleEndian.PutUint32(hdr[76:80], nonce)
}

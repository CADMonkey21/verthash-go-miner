package cuda

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ebitengine/purego"
)

const (
	NVML_SUCCESS         = 0
	NVML_TEMPERATURE_GPU = 0
)

var (
	nvmlReady                  bool
	nvmlInitV2                 func() uint32
	nvmlDeviceGetCountV2       func(count *uint32) uint32
	nvmlDeviceGetHandleByIdxV2 func(index uint32, device *uintptr) uint32
	nvmlDeviceGetName          func(device uintptr, name *byte, length uint32) uint32
	nvmlDeviceGetTemperature   func(device uintptr, sensorType uint32, temp *uint32) uint32
	nvmlDeviceGetFanSpeed      func(device uintptr, speed *uint32) uint32
	nvmlDeviceGetPowerUsage    func(device uintptr, powerMilliwatts *uint32) uint32
)

type GPUTelemetry struct {
	TempC    uint32
	FanPct   uint32
	PowerW   float64
	HasTemp  bool
	HasFan   bool
	HasPower bool
}

func InitNVML(cudaDevices []DeviceInfo) map[int32]uintptr {
	handles := make(map[int32]uintptr)
	lib, err := openNVMLLibrary()
	if err != nil {
		return handles
	}

	purego.RegisterLibFunc(&nvmlInitV2, lib, "nvmlInit_v2")
	purego.RegisterLibFunc(&nvmlDeviceGetCountV2, lib, "nvmlDeviceGetCount_v2")
	purego.RegisterLibFunc(&nvmlDeviceGetHandleByIdxV2, lib, "nvmlDeviceGetHandleByIndex_v2")
	purego.RegisterLibFunc(&nvmlDeviceGetName, lib, "nvmlDeviceGetName")
	purego.RegisterLibFunc(&nvmlDeviceGetTemperature, lib, "nvmlDeviceGetTemperature")
	purego.RegisterLibFunc(&nvmlDeviceGetFanSpeed, lib, "nvmlDeviceGetFanSpeed")
	purego.RegisterLibFunc(&nvmlDeviceGetPowerUsage, lib, "nvmlDeviceGetPowerUsage")

	if res := nvmlInitV2(); res != NVML_SUCCESS {
		return handles
	}
	nvmlReady = true

	var count uint32
	if nvmlDeviceGetCountV2(&count) != NVML_SUCCESS || count == 0 {
		return handles
	}

	type nvmlEntry struct {
		handle uintptr
		name   string
		used   bool
	}
	entries := make([]nvmlEntry, 0, count)
	for i := uint32(0); i < count; i++ {
		var h uintptr
		if nvmlDeviceGetHandleByIdxV2(i, &h) == NVML_SUCCESS {
			var nameBuf [96]byte
			var nameStr string
			if nvmlDeviceGetName(h, &nameBuf[0], uint32(len(nameBuf))) == NVML_SUCCESS {
				if idx := bytes.IndexByte(nameBuf[:], 0); idx >= 0 {
					nameStr = string(nameBuf[:idx])
				}
			}
			entries = append(entries, nvmlEntry{handle: h, name: strings.TrimSpace(nameStr)})
		}
	}

	for _, d := range cudaDevices {
		matched := false
		for i := range entries {
			if !entries[i].used && strings.EqualFold(entries[i].name, strings.TrimSpace(d.Name)) {
				handles[d.Index] = entries[i].handle
				entries[i].used = true
				matched = true
				break
			}
		}
		if !matched && int(d.Index) < len(entries) && !entries[d.Index].used {
			handles[d.Index] = entries[d.Index].handle
			entries[d.Index].used = true
		}
	}
	return handles
}

func QueryTelemetry(handle uintptr) GPUTelemetry {
	var t GPUTelemetry
	if !nvmlReady || handle == 0 {
		return t
	}
	var temp uint32
	if nvmlDeviceGetTemperature(handle, NVML_TEMPERATURE_GPU, &temp) == NVML_SUCCESS {
		t.TempC = temp
		t.HasTemp = true
	}
	var fan uint32
	if nvmlDeviceGetFanSpeed(handle, &fan) == NVML_SUCCESS {
		t.FanPct = fan
		t.HasFan = true
	}
	var mw uint32
	if nvmlDeviceGetPowerUsage(handle, &mw) == NVML_SUCCESS {
		t.PowerW = float64(mw) / 1000.0
		t.HasPower = true
	}
	return t
}

func (t GPUTelemetry) FormatCompact() string {
	var parts []string
	if t.HasTemp {
		parts = append(parts, fmt.Sprintf("%dC", t.TempC))
	}
	if t.HasFan {
		parts = append(parts, fmt.Sprintf("Fan:%d%%", t.FanPct))
	}
	if t.HasPower {
		parts = append(parts, fmt.Sprintf("%.0fW", t.PowerW))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, " ") + ")"
}

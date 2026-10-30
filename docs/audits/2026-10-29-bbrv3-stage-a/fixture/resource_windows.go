package main

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

func usage() resources {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &creation, &exit, &kernel, &user); err != nil {
		return resources{Error: err.Error()}
	}
	ticks := func(t windows.Filetime) uint64 { return uint64(t.HighDateTime)<<32 | uint64(t.LowDateTime) }
	// PROCESS_MEMORY_COUNTERS layout from the Windows SDK (SIZE_T fields).
	var mem struct {
		Size, Faults                                                                    uint32
		Peak, Working, PeakPaged, Paged, PeakNonPaged, NonPaged, Pagefile, PeakPagefile uintptr
	}
	mem.Size = uint32(unsafe.Sizeof(mem))
	ok, _, err := windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo").Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&mem)), uintptr(mem.Size))
	result := resources{CPUSeconds: float64(ticks(kernel)+ticks(user)) / 1e7, PeakRSSBytes: uint64(mem.Peak)}
	if ok == 0 {
		result.Error = err.Error()
	}
	return result
}

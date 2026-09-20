//go:build windows

package monitor

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type memoryStatusEx struct {
	cbSize                  uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

var (
	kernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetSystemTimes       = kernel32.NewProc("GetSystemTimes")

	cpuMu      sync.Mutex
	prevIdle   windows.Filetime
	prevKernel windows.Filetime
	prevUser   windows.Filetime
	hasPrev    bool
)

func filetimeToUint64(ft *windows.Filetime) uint64 {
	return (uint64(ft.HighDateTime) << 32) | uint64(ft.LowDateTime)
}

func systemSample() (cpu, mem float64, ok bool) {
	// Memory
	var ms memoryStatusEx
	ms.cbSize = uint32(unsafe.Sizeof(ms))
	r1, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms)))
	if r1 != 0 {
		mem = float64(ms.dwMemoryLoad)
	}

	// CPU
	cpuMu.Lock()
	defer cpuMu.Unlock()
	var idle, kernel, user windows.Filetime
	r2, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r2 != 0 {
		if !hasPrev {
			prevIdle = idle
			prevKernel = kernel
			prevUser = user
			hasPrev = true
			cpu = 0.0
		} else {
			idleDiff := filetimeToUint64(&idle) - filetimeToUint64(&prevIdle)
			kernelDiff := filetimeToUint64(&kernel) - filetimeToUint64(&prevKernel)
			userDiff := filetimeToUint64(&user) - filetimeToUint64(&prevUser)
			prevIdle = idle
			prevKernel = kernel
			prevUser = user

			total := kernelDiff + userDiff
			if total > 0 && total >= idleDiff {
				busy := total - idleDiff
				cpu = float64(busy) / float64(total) * 100
				if cpu > 100 {
					cpu = 100
				}
				if cpu < 0 {
					cpu = 0
				}
			}
		}
	}
	return cpu, mem, true
}

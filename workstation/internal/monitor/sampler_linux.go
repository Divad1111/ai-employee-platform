//go:build linux

package monitor

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
)

var (
	cpuMu     sync.Mutex
	prevTotal uint64
	prevIdle  uint64
	hasPrev   bool
)

func systemSample() (cpu, mem float64, ok bool) {
	// /proc/meminfo
	if f, err := os.Open("/proc/meminfo"); err == nil {
		scanner := bufio.NewScanner(f)
		var total, avail uint64
		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				if parts[0] == "MemTotal:" {
					total, _ = strconv.ParseUint(parts[1], 10, 64)
				} else if parts[0] == "MemAvailable:" {
					avail, _ = strconv.ParseUint(parts[1], 10, 64)
				}
			}
		}
		_ = f.Close()
		if total > 0 && total >= avail {
			mem = float64(total-avail) / float64(total) * 100
		}
	}

	// /proc/stat
	if f, err := os.Open("/proc/stat"); err == nil {
		scanner := bufio.NewScanner(f)
		if scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 5 && fields[0] == "cpu" {
				var curTotal uint64
				for i := 1; i < len(fields); i++ {
					v, _ := strconv.ParseUint(fields[i], 10, 64)
					curTotal += v
				}
				curIdle, _ := strconv.ParseUint(fields[4], 10, 64)

				cpuMu.Lock()
				if !hasPrev {
					prevTotal = curTotal
					prevIdle = curIdle
					hasPrev = true
					cpu = 0.0
				} else {
					diffTotal := curTotal - prevTotal
					diffIdle := curIdle - prevIdle
					prevTotal = curTotal
					prevIdle = curIdle
					if diffTotal > 0 && diffTotal >= diffIdle {
						cpu = float64(diffTotal-diffIdle) / float64(diffTotal) * 100
					}
				}
				cpuMu.Unlock()
			}
		}
		_ = f.Close()
	}

	return cpu, mem, true
}

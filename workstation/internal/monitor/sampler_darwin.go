//go:build darwin

package monitor

import (
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

var (
	ncpuOnce sync.Once
	ncpu     = 1.0
)

func systemSample() (cpu, mem float64, ok bool) {
	return darwinCPU(), darwinMemory(), true
}

func diskSample() float64 {
	var st unix.Statfs_t
	if err := unix.Statfs("/", &st); err != nil || st.Blocks == 0 {
		return 0
	}
	used := st.Blocks - st.Bavail
	return float64(used) / float64(st.Blocks) * 100
}

func darwinCPU() float64 {
	ncpuOnce.Do(func() {
		if v, err := unix.SysctlUint32("hw.ncpu"); err == nil && v > 0 {
			ncpu = float64(v)
		}
	})
	out, err := exec.Command("ps", "-A", "-o", "%cpu=").Output()
	if err != nil {
		return 0
	}
	var sum float64
	for _, line := range strings.Fields(string(out)) {
		n, err := strconv.ParseFloat(line, 64)
		if err == nil && n > 0 {
			sum += n
		}
	}
	pct := sum / ncpu
	if pct > 100 {
		pct = 100
	}
	return pct
}

func darwinMemory() float64 {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil || total == 0 {
		return 0
	}
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0
	}
	page := uint64(16384)
	pages := map[string]uint64{}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "page size of") {
			fields := strings.Fields(line)
			for i, f := range fields {
				if f == "size" && i+2 < len(fields) {
					n, _ := strconv.ParseUint(fields[i+2], 10, 64)
					if n > 0 {
						page = n
					}
				}
			}
			continue
		}
		name, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		raw := strings.Trim(strings.TrimSpace(val), ".")
		n, err := strconv.ParseUint(strings.ReplaceAll(raw, ".", ""), 10, 64)
		if err != nil {
			continue
		}
		pages[strings.TrimSpace(name)] = n
	}
	usedPages := pages["Pages active"] + pages["Pages wired down"] + pages["Pages occupied by compressor"]
	used := usedPages * page
	if used > total {
		used = total
	}
	return float64(used) / float64(total) * 100
}

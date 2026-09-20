//go:build !windows && !linux

package monitor

func systemSample() (cpu, mem float64, ok bool) {
	return 0, 0, false
}

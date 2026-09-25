//go:build !darwin && !linux

package monitor

func diskSample() float64 { return 0 }

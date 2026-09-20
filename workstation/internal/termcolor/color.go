package termcolor

import (
	"os"
	"runtime"
	"sync"
)

var (
	initOnce sync.Once
	enabled  bool
)

func IsEnabled() bool {
	initOnce.Do(func() {
		if os.Getenv("NO_COLOR") != "" {
			enabled = false
			return
		}
		if runtime.GOOS == "windows" {
			enabled = enableWindowsVT()
		} else {
			term := os.Getenv("TERM")
			enabled = term != "" && term != "dumb"
		}
	})
	return enabled
}

func Green(s string) string {
	if !IsEnabled() {
		return s
	}
	return "\033[32m" + s + "\033[0m"
}

func Red(s string) string {
	if !IsEnabled() {
		return s
	}
	return "\033[31m" + s + "\033[0m"
}

func Yellow(s string) string {
	if !IsEnabled() {
		return s
	}
	return "\033[33m" + s + "\033[0m"
}

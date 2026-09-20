//go:build !windows

package termcolor

func enableWindowsVT() bool {
	return false
}

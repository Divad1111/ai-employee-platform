//go:build windows

package platform

func isNonRoot() bool {
	return true
}

//go:build !windows

package platform

import "os"

func isNonRoot() bool {
	return os.Geteuid() != 0
}

//go:build windows

package platform

import (
	"os"
	"strings"
)

func isNonRoot() bool {
	home, _ := os.UserHomeDir()
	if strings.Contains(strings.ToLower(home), "systemprofile") {
		return false
	}
	return true
}

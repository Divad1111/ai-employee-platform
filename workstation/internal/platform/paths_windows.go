//go:build windows

package platform

import (
	"os"
	"path/filepath"
	"strings"
)

func isNonRoot() bool {
	home, _ := os.UserHomeDir()
	if strings.Contains(strings.ToLower(home), "systemprofile") {
		return false
	}
	return true
}

func findFirstUserAie() string {
	sysDrive := os.Getenv("SystemDrive")
	if sysDrive == "" {
		sysDrive = "C:"
	}
	usersDir := filepath.Join(sysDrive, "\\Users")
	entries, err := os.ReadDir(usersDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "Public" || name == "Default" || name == "Default User" || name == "All Users" {
			continue
		}
		candidate := filepath.Join(usersDir, name, ".aie")
		idFile := filepath.Join(candidate, "identity", "workstation-id")
		if _, err := os.Stat(idFile); err == nil {
			return candidate
		}
	}
	return ""
}

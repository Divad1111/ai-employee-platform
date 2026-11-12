//go:build !windows

package runtime

import "os"

func redirectedDirectory(os.FileInfo) bool { return false }

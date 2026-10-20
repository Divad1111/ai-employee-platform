// Package jobinput defines the transport contract for user-supplied job files.
package jobinput

import (
	"fmt"
	"path/filepath"
	"strings"
)

const MaxBytes int64 = 32 << 20
const MaxTotalBytes int64 = 64 << 20
const MaxFiles = 10

type File struct {
	SHA256   string `json:"sha256"`
	Name     string `json:"name"`
	MIMEType string `json:"mime_type"`
	Size     int64  `json:"size"`
}

func ValidHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, ch := range hash {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

// SafeName is portable to Windows and preserves Chinese filenames. No path from
// Feishu is ever used as a directory, alternate data stream or executable name.
func SafeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(ch rune) rune {
		if ch < 32 || strings.ContainsRune(`<>:"/\|?*`, ch) {
			return '_'
		}
		return ch
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		name = "attachment.bin"
	}
	runes := []rune(name)
	if len(runes) > 120 {
		name = string(runes[len(runes)-120:])
	}
	return name
}

func DownloadPayload(wsID, timestamp, jobID, hash string) string {
	return strings.Join([]string{"aie-job-input-v1", wsID, timestamp, jobID, hash}, "\n")
}

func Validate(files []File) error {
	if len(files) > MaxFiles {
		return fmt.Errorf("每条任务最多 %d 个附件", MaxFiles)
	}
	var total int64
	for _, f := range files {
		if !ValidHash(f.SHA256) || f.Size <= 0 || f.Size > MaxBytes || f.Name != SafeName(f.Name) {
			return fmt.Errorf("附件信息无效或超过 32 MiB 限制")
		}
		total += f.Size
	}
	if total > MaxTotalBytes {
		return fmt.Errorf("任务附件总大小超过 64 MiB")
	}
	return nil
}

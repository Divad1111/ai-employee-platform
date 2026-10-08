// 上传策略：文件名消毒、类型白名单、体积上限。
package artifact

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// DefaultMaxBytes 单文件默认上限 32 MiB。
	DefaultMaxBytes int64 = 32 << 20
	// MaxNameLen 文件名最大长度。
	MaxNameLen = 180
)

var (
	ErrTooLarge      = errors.New("制品超过大小上限")
	ErrInvalidName   = errors.New("非法文件名")
	ErrInvalidType   = errors.New("不支持的制品类型")
	ErrJobRequired   = errors.New("需要有效 job_id")
	ErrJobForbidden  = errors.New("无权为该 Job 上传制品")
	ErrEmptyBody     = errors.New("空文件")
)

var safeNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,178}$`)

// allowedTypes 显式白名单；空 type 归一为 bin。
var allowedTypes = map[string]bool{
	"bin": true, "json": true, "txt": true, "log": true, "md": true, "markdown": true,
	"diff": true, "patch": true, "zip": true, "tar": true, "gz": true,
	"png": true, "jpg": true, "jpeg": true, "webp": true,
	"apk": true, "ipa": true, "result": true, "report": true,
}

// SanitizeName 仅保留 basename，拒绝路径穿越与危险字符。
func SanitizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrInvalidName
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return "", ErrInvalidName
	}
	base := filepath.Base(name)
	if base == "." || base == ".." {
		return "", ErrInvalidName
	}
	if !utf8.ValidString(base) || len(base) > MaxNameLen {
		return "", ErrInvalidName
	}
	if !safeNameRe.MatchString(base) {
		return "", ErrInvalidName
	}
	return base, nil
}

// NormalizeType 归一化并校验类型白名单。
func NormalizeType(typ string) (string, error) {
	t := strings.ToLower(strings.TrimSpace(typ))
	if t == "" {
		t = "bin"
	}
	if !allowedTypes[t] {
		return "", ErrInvalidType
	}
	return t, nil
}

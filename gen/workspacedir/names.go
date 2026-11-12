// Package workspacedir validates directory names shared by the control plane and workstation.
package workspacedir

import (
	"errors"
	"strings"
	"unicode"
)

func ValidName(name string) bool {
	if name == "" || name == "." || strings.Contains(name, "..") || strings.HasSuffix(name, ".") || strings.TrimSpace(name) != name {
		return false
	}
	for _, c := range name {
		if unicode.IsControl(c) || strings.ContainsRune(`<>:"/\|?*~`, c) {
			return false
		}
	}
	stem, _, _ := strings.Cut(strings.ToUpper(name), ".")
	switch strings.TrimRight(stem, " ") {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$", "CLOCK$":
		return false
	}
	for _, prefix := range []string{"COM", "LPT"} {
		if n, ok := strings.CutPrefix(strings.TrimRight(stem, " "), prefix); ok && len([]rune(n)) == 1 && strings.Contains("0123456789¹²³", n) {
			return false
		}
	}
	return true
}

// Roots are interpreted on the worker, which may use a different OS from the server.
func ValidateRoots(roots []string) error {
	var seen []string
	for _, root := range roots {
		p := root
		drive := len(p) >= 3 && ((p[0] >= 'a' && p[0] <= 'z') || (p[0] >= 'A' && p[0] <= 'Z')) && p[1] == ':' && (p[2] == '/' || p[2] == '\\')
		if drive {
			p = strings.ToLower(strings.ReplaceAll(p, "\\", "/"))
		} else if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
			return errors.New("根目录必须是本机绝对路径")
		}
		rest := p[1:]
		if drive {
			rest = p[3:]
		}
		if rest != "" {
			for _, part := range strings.Split(strings.TrimSuffix(rest, "/"), "/") {
				if part == "" || part == "." || part == ".." || strings.IndexFunc(part, unicode.IsControl) >= 0 || (drive && (strings.ContainsAny(part, `<>:"|?*`) || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " "))) {
					return errors.New("根目录包含无效路径组件")
				}
			}
		}
		key := strings.TrimRight(p, "/") + "/"
		for _, other := range seen {
			if strings.HasPrefix(key, other) || strings.HasPrefix(other, key) {
				return errors.New("根目录不能重复或互相嵌套")
			}
		}
		seen = append(seen, key)
	}
	return nil
}

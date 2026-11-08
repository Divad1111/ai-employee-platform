// Package runtimeenv 限制 Agent 子进程继承的环境，业务密钥必须显式注入。
package runtimeenv

import (
	"os"
	"strings"
)

func Base() []string {
	allowed := map[string]bool{"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "TMPDIR": true, "TMP": true, "TEMP": true, "SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true, "PATHEXT": true, "USERPROFILE": true, "APPDATA": true, "LOCALAPPDATA": true, "LANG": true, "LC_ALL": true, "TERM": true}
	out := []string{}
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok && allowed[strings.ToUpper(key)] {
			out = append(out, entry)
		}
	}
	return out
}

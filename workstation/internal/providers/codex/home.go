package codex

import (
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// applyCodexEnv 让子进程使用已登录用户的 Codex 目录。
// aew 以 LocalSystem 运行时，自己的用户目录里没有 codex login 写入的 auth.json。
func applyCodexEnv(cmd *exec.Cmd) {
	home := resolveCodexHome()
	if home == "" || cmd == nil {
		return
	}
	cmd.Env = append(os.Environ(), "CODEX_HOME="+home)
}

func resolveCodexHome() string {
	if h := os.Getenv("CODEX_HOME"); hasAuth(h) {
		return h
	}
	if userHome, err := os.UserHomeDir(); err == nil && hasAuth(filepath.Join(userHome, ".codex")) {
		return filepath.Join(userHome, ".codex")
	}
	return newestCodexHome(userProfileCodexDirs())
}

func userProfileCodexDirs() []string {
	sysDrive := os.Getenv("SystemDrive")
	if sysDrive == "" {
		sysDrive = "C:"
	}
	root := filepath.Join(sysDrive, "Users")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	skip := map[string]bool{"Public": true, "Default": true, "Default User": true, "All Users": true}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || skip[entry.Name()] {
			continue
		}
		out = append(out, filepath.Join(root, entry.Name(), ".codex"))
	}
	return out
}

func newestCodexHome(dirs []string) string {
	var best string
	var bestTime time.Time
	for _, dir := range dirs {
		st, err := os.Stat(filepath.Join(dir, "auth.json"))
		if err != nil || st.IsDir() || st.Size() == 0 {
			continue
		}
		if best == "" || st.ModTime().After(bestTime) {
			best = dir
			bestTime = st.ModTime()
		}
	}
	return best
}

func hasAuth(dir string) bool {
	if dir == "" {
		return false
	}
	st, err := os.Stat(filepath.Join(dir, "auth.json"))
	return err == nil && !st.IsDir() && st.Size() > 0
}

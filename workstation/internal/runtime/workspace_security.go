package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func validResourceID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, "/\\:\x00")
}

// secureWorkspacePath 只接受该员工专属根目录的子目录，并拒绝内部符号链接。
// 这限制平台可选择的工作目录；互不信任的租户仍需独立 OS 账号/VM。
func (m *Managers) secureWorkspacePath(employeeID, path string) (string, error) {
	if !validResourceID(employeeID) {
		return "", fmt.Errorf("非法员工 ID")
	}
	root, err := filepath.Abs(m.Paths.WorkspaceRoot())
	if err != nil {
		return "", err
	}
	base := filepath.Join(root, employeeID)
	if path == "" {
		path = filepath.Join(base, "workspace")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("工作区必须是员工专属目录内的绝对路径")
	}
	path = filepath.Clean(path)
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("工作区超出员工专属目录：%s", base)
	}
	// 先检查所有已存在组件，再创建目录，避免通过预先放置的链接写入外部路径。
	current := root
	for _, part := range strings.Split(filepath.Join(employeeID, rel), string(filepath.Separator)) {
		if part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if e != nil && !os.IsNotExist(e) {
			return "", e
		}
		if e == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return "", fmt.Errorf("工作区不能包含符号链接或普通文件")
		}
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	confined, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer confined.Close()
	if err := confined.MkdirAll(filepath.Join(employeeID, rel), 0o700); err != nil {
		return "", err
	}
	return path, nil
}

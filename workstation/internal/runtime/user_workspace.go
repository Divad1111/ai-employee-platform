package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ai-employee-platform/gen/workspacedir"
)

// Walk from the volume root and perform directory operations relative to open
// handles. Reject redirects anywhere along the selected root and project path.
func managedWorkspacePath(root, user, name string) (string, error) {
	if !filepath.IsAbs(root) || workspacedir.ValidateRoots([]string{root}) != nil || !workspacedir.ValidName(user) || !workspacedir.ValidName(name) {
		return "", errors.New("无效的工作站根目录、用户目录或工作区目录名")
	}
	full := filepath.Join(root, user, name)
	// Open the nearest existing ancestor. Opening every drive ancestor for
	// directory creation would require write access to C:\\Users on Windows.
	base := filepath.Clean(root)
	for {
		_, err := os.Lstat(base)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(base)
		if parent == base {
			return "", err
		}
		base = parent
	}
	for path := base; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || redirectedDirectory(info) {
			return "", fmt.Errorf("根目录包含重解析点或链接：%s", path)
		}
		if path == filepath.Dir(path) {
			break
		}
	}
	relative, err := filepath.Rel(base, full)
	if err != nil {
		return "", err
	}
	dir, err := os.OpenRoot(base)
	if err != nil {
		return "", err
	}
	defer func() { dir.Close() }()
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		info, err := dir.Lstat(part)
		if errors.Is(err, os.ErrNotExist) {
			if err = dir.Mkdir(part, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				return "", err
			}
			info, err = dir.Lstat(part)
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || redirectedDirectory(info) {
			return "", fmt.Errorf("目录不能是链接、重解析点或普通文件：%s", part)
		}
		child, err := dir.OpenRoot(part)
		if err != nil {
			return "", err
		}
		opened, err := child.Stat(".")
		if err != nil || !os.SameFile(info, opened) {
			child.Close()
			return "", fmt.Errorf("目录在打开时已改变：%s", part)
		}
		dir.Close()
		dir = child
	}
	return full, nil
}

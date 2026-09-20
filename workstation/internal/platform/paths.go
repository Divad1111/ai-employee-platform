// Package platform 定义跨平台路径抽象。
// 设计依据：设计文档 §45–§48。
package platform

import (
	"os"
	"path/filepath"
	"runtime"
)

// Paths 描述本机程序/配置/数据/身份/日志/工作区根路径。
type Paths interface {
	ProgramDir() string
	ConfigDir() string
	DataDir() string
	IdentityDir() string
	LogDir() string
	WorkspaceRoot() string
}

// Detect 按当前 OS 返回路径实现；可用 AIE_DATA_DIR 覆盖数据根（便于测试）。
// 智能自适应机制：
// 1. 若显式指定 AIE_DATA_DIR，优先使用；
// 2. 若当前用户主目录下已存在 ~/.aie/identity/workstation-id，自动定位使用 ~/.aie；
// 3. 若系统级标准路径已存在 workstation-id，使用系统级路径；
// 4. 未注册状态下：普通非 root 用户默认定位至 ~/.aie，免 sudo 与权限污染。
func Detect() Paths {
	if root := os.Getenv("AIE_DATA_DIR"); root != "" {
		return &devPaths{root: root}
	}

	home, _ := os.UserHomeDir()
	userAie := ""
	if home != "" {
		userAie = filepath.Join(home, ".aie")
	}

	// 1. 优先检测当前用户目录下的已注册身份
	if userAie != "" {
		if _, err := os.Stat(filepath.Join(userAie, "identity", "workstation-id")); err == nil {
			return &devPaths{root: userAie}
		}
	}

	// 1.5. 若在系统服务/SYSTEM账户下运行且自身无身份，自动查找宿主机已有用户注册的身份
	if uRoot := findFirstUserAie(); uRoot != "" {
		return &devPaths{root: uRoot}
	}

	// 2. 检查系统标准路径是否存在已注册身份
	var sysPaths Paths
	switch runtime.GOOS {
	case "windows":
		sysPaths = WindowsPaths{}
	case "darwin":
		sysPaths = DarwinPaths{}
	default:
		sysPaths = LinuxPaths{}
	}
	if _, err := os.Stat(filepath.Join(sysPaths.IdentityDir(), "workstation-id")); err == nil {
		return sysPaths
	}

	// 3. 首次未注册场景：普通用户优先默认使用 ~/.aie
	if userAie != "" && isNonRoot() {
		return &devPaths{root: userAie}
	}

	return sysPaths
}

// WindowsPaths Windows 路径（§45）。
type WindowsPaths struct{}

func (WindowsPaths) ProgramDir() string {
	return filepath.Join(os.Getenv("ProgramFiles"), "AIEmployee")
}
func (WindowsPaths) ConfigDir() string {
	return filepath.Join(os.Getenv("ProgramData"), "AIEmployee", "config")
}
func (WindowsPaths) DataDir() string {
	return filepath.Join(os.Getenv("ProgramData"), "AIEmployee", "data")
}
func (WindowsPaths) IdentityDir() string {
	return filepath.Join(os.Getenv("ProgramData"), "AIEmployee", "identity")
}
func (WindowsPaths) LogDir() string {
	return filepath.Join(os.Getenv("ProgramData"), "AIEmployee", "data", "logs")
}
func (WindowsPaths) WorkspaceRoot() string {
	return `D:\AIEmployees`
}

// LinuxPaths Linux 路径（§46）。
type LinuxPaths struct{}

func (LinuxPaths) ProgramDir() string    { return "/opt/aie" }
func (LinuxPaths) ConfigDir() string     { return "/etc/aie" }
func (LinuxPaths) DataDir() string       { return "/var/lib/aie" }
func (LinuxPaths) IdentityDir() string   { return "/var/lib/aie/identity" }
func (LinuxPaths) LogDir() string        { return "/var/log/aie" }
func (LinuxPaths) WorkspaceRoot() string { return "/var/lib/aie/employees" }

// DarwinPaths macOS 路径（§47）。
type DarwinPaths struct{}

func (DarwinPaths) ProgramDir() string {
	return "/Library/Application Support/AIEmployee"
}
func (DarwinPaths) ConfigDir() string {
	return "/Library/Application Support/AIEmployee/config"
}
func (DarwinPaths) DataDir() string {
	return "/Library/Application Support/AIEmployee/data"
}
func (DarwinPaths) IdentityDir() string {
	return "/Library/Application Support/AIEmployee/identity"
}
func (DarwinPaths) LogDir() string {
	return "/Library/Application Support/AIEmployee/logs"
}
func (DarwinPaths) WorkspaceRoot() string {
	return "/Users/Shared/AIEmployees"
}

// 测试/开发覆盖路径。
type devPaths struct{ root string }

func (d *devPaths) ProgramDir() string    { return filepath.Join(d.root, "program") }
func (d *devPaths) ConfigDir() string     { return filepath.Join(d.root, "config") }
func (d *devPaths) DataDir() string       { return filepath.Join(d.root, "data") }
func (d *devPaths) IdentityDir() string   { return filepath.Join(d.root, "identity") }
func (d *devPaths) LogDir() string        { return filepath.Join(d.root, "logs") }
func (d *devPaths) WorkspaceRoot() string { return filepath.Join(d.root, "employees") }

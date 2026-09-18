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
func Detect() Paths {
	if root := os.Getenv("AIE_DATA_DIR"); root != "" {
		return &devPaths{root: root}
	}
	switch runtime.GOOS {
	case "windows":
		return WindowsPaths{}
	case "darwin":
		return DarwinPaths{}
	default:
		return LinuxPaths{}
	}
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

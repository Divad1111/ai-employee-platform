// Package service 将 aew daemon 注册为 OS 服务。
// 设计依据：设计文档 §69；决策 Q-05。
package service

import (
	"fmt"
	"runtime"
)

// Manager OS 服务管理接口。
type Manager interface {
	Install(binPath string) error
	Uninstall() error
	Start() error
	Stop() error
	Restart() error
	Status() (string, error)
}

// New 按平台返回实现。
func New() Manager {
	return platformManager()
}

// Unsupported 统一「未实现」返回。
type Unsupported struct{ Platform string }

func (u Unsupported) Install(string) error { return u.err("install") }
func (u Unsupported) Uninstall() error     { return u.err("uninstall") }
func (u Unsupported) Start() error         { return u.err("start") }
func (u Unsupported) Stop() error          { return u.err("stop") }
func (u Unsupported) Restart() error       { return u.err("restart") }
func (u Unsupported) Status() (string, error) {
	return "unsupported", u.err("status")
}
func (u Unsupported) err(op string) error {
	return fmt.Errorf("aew service %s 在 %s 尚未实现完整安装器；请使用 `aew daemon` 前台运行", op, u.Platform)
}

func platformManager() Manager {
	switch runtime.GOOS {
	case "windows":
		return WindowsService{Name: "AIEmployeeWorkstation"}
	case "linux":
		return SystemdService{Unit: "aie-aew.service"}
	case "darwin":
		return LaunchdService{Label: "com.aie.aew"}
	default:
		return Unsupported{Platform: runtime.GOOS}
	}
}

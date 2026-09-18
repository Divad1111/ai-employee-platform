//go:build windows

package service

import (
	"fmt"
	"os"
	"os/exec"
)

// WindowsService 使用 sc.exe 注册服务（需管理员）。
type WindowsService struct {
	Name string
}

func (w WindowsService) Install(binPath string) error {
	if binPath == "" {
		var err error
		binPath, err = os.Executable()
		if err != nil {
			return err
		}
	}
	// binPath daemon
	cmd := exec.Command("sc", "create", w.Name, "binPath=", fmt.Sprintf("\"%s\" daemon", binPath), "start=", "auto")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sc create: %w (%s)", err, string(out))
	}
	return nil
}

func (w WindowsService) Uninstall() error {
	out, err := exec.Command("sc", "delete", w.Name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sc delete: %w (%s)", err, string(out))
	}
	return nil
}

func (w WindowsService) Start() error {
	out, err := exec.Command("sc", "start", w.Name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sc start: %w (%s)", err, string(out))
	}
	return nil
}

func (w WindowsService) Stop() error {
	out, err := exec.Command("sc", "stop", w.Name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sc stop: %w (%s)", err, string(out))
	}
	return nil
}

func (w WindowsService) Restart() error {
	_ = w.Stop()
	return w.Start()
}

func (w WindowsService) Status() (string, error) {
	out, err := exec.Command("sc", "query", w.Name).CombinedOutput()
	return string(out), err
}

// SystemdService / LaunchdService 在 windows 构建中的占位（不会选用）。
type SystemdService struct{ Unit string }
type LaunchdService struct{ Label string }

func (SystemdService) Install(string) error      { return Unsupported{Platform: "linux"}.Install("") }
func (SystemdService) Uninstall() error          { return Unsupported{Platform: "linux"}.Uninstall() }
func (SystemdService) Start() error              { return Unsupported{Platform: "linux"}.Start() }
func (SystemdService) Stop() error               { return Unsupported{Platform: "linux"}.Stop() }
func (SystemdService) Restart() error            { return Unsupported{Platform: "linux"}.Restart() }
func (SystemdService) Status() (string, error)   { return Unsupported{Platform: "linux"}.Status() }
func (LaunchdService) Install(string) error      { return Unsupported{Platform: "darwin"}.Install("") }
func (LaunchdService) Uninstall() error          { return Unsupported{Platform: "darwin"}.Uninstall() }
func (LaunchdService) Start() error              { return Unsupported{Platform: "darwin"}.Start() }
func (LaunchdService) Stop() error               { return Unsupported{Platform: "darwin"}.Stop() }
func (LaunchdService) Restart() error            { return Unsupported{Platform: "darwin"}.Restart() }
func (LaunchdService) Status() (string, error)   { return Unsupported{Platform: "darwin"}.Status() }

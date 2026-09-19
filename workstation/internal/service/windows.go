//go:build windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	binPath, _ = filepath.Abs(binPath)
	// binPath daemon
	cmd := exec.Command("sc", "create", w.Name, "binPath=", fmt.Sprintf("\"%s\" daemon", binPath), "start=", "auto")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sc create: %w (%s)", err, string(out))
	}
	if link, err := createGlobalSymlink(binPath); err == nil {
		fmt.Printf("已建立全局 PATH 命令: %s -> %s\n", link, binPath)
	}
	return nil
}

func (w WindowsService) Uninstall() error {
	out, err := exec.Command("sc", "delete", w.Name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sc delete: %w (%s)", err, string(out))
	}
	removeGlobalSymlink()
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

func createGlobalSymlink(binPath string) (string, error) {
	if binPath == "" {
		var err error
		binPath, err = os.Executable()
		if err != nil {
			return "", err
		}
	}
	binPath, _ = filepath.Abs(binPath)

	sysRoot := os.Getenv("SystemRoot")
	if sysRoot == "" {
		sysRoot = "C:\\Windows"
	}

	// 1. 尝试直接在 System32 下建立硬链接或符号链接
	targetExe := filepath.Join(sysRoot, "System32", "aew.exe")
	_ = os.Remove(targetExe)
	if err := os.Link(binPath, targetExe); err == nil {
		return targetExe, nil
	}
	if err := os.Symlink(binPath, targetExe); err == nil {
		return targetExe, nil
	}

	// 2. 尝试在 SystemRoot (C:\Windows) 下建立 aew.cmd 代理脚本
	targetCmd := filepath.Join(sysRoot, "aew.cmd")
	content := fmt.Sprintf("@echo off\r\n\"%s\" %%*\r\n", binPath)
	if err := os.WriteFile(targetCmd, []byte(content), 0o755); err == nil {
		return targetCmd, nil
	}

	// 3. 尝试在当前用户 %USERPROFILE%\.local\bin
	home, _ := os.UserHomeDir()
	if home != "" {
		userBin := filepath.Join(home, ".local", "bin")
		_ = os.MkdirAll(userBin, 0o755)
		userCmd := filepath.Join(userBin, "aew.cmd")
		if err := os.WriteFile(userCmd, []byte(content), 0o755); err == nil {
			return userCmd, nil
		}
	}

	return "", fmt.Errorf("无法在 Windows 系统 PATH 建立命令软链接")
}

func removeGlobalSymlink() {
	sysRoot := os.Getenv("SystemRoot")
	if sysRoot == "" {
		sysRoot = "C:\\Windows"
	}
	_ = os.Remove(filepath.Join(sysRoot, "System32", "aew.exe"))
	_ = os.Remove(filepath.Join(sysRoot, "aew.cmd"))
	home, _ := os.UserHomeDir()
	if home != "" {
		_ = os.Remove(filepath.Join(home, ".local", "bin", "aew.cmd"))
	}
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

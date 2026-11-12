//go:build windows

package service

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// WindowsService 管理当前 Windows 登录用户的计划任务。
type WindowsService struct {
	Name string
}

func decodeOutput(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	dec, err := simplifiedchinese.GBK.NewDecoder().Bytes(b)
	if err == nil {
		return string(dec)
	}
	return string(b)
}

//go:embed user_task.ps1
var userTaskScript string

//go:embed user_daemon.ps1
var userDaemonScript string

func (w WindowsService) userTask(op, bin string) (string, error) {
	systemRoot := os.Getenv("SystemRoot")
	powershell := filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.Command(powershell, "-NoProfile", "-NonInteractive", "-Command", userTaskScript)
	cmd.Env = append(os.Environ(), "AIE_TASK_OPERATION="+op, "AIE_TASK_BINARY="+bin, "AIE_TASK_SERVICE="+w.Name,
		"AIE_TASK_LAUNCHER="+base64.StdEncoding.EncodeToString([]byte(userDaemonScript)))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	raw, err := cmd.CombinedOutput()
	out := strings.TrimSpace(decodeOutput(raw))
	if err != nil {
		return out, fmt.Errorf("Windows 登录任务 %s 失败：%s (%w)", op, out, err)
	}
	return out, nil
}
func (w WindowsService) Install(binPath string) error {
	var err error
	if binPath == "" {
		binPath, err = os.Executable()
		if err != nil {
			return err
		}
	}
	binPath, err = filepath.EvalSymlinks(binPath)
	if err != nil {
		return err
	}
	binPath, err = filepath.Abs(binPath)
	if err != nil {
		return err
	}
	_, err = w.userTask("install", binPath)
	return err
}
func (w WindowsService) Uninstall() error        { _, err := w.userTask("uninstall", ""); return err }
func (w WindowsService) Start() error            { _, err := w.userTask("start", ""); return err }
func (w WindowsService) Stop() error             { _, err := w.userTask("stop", ""); return err }
func (w WindowsService) Restart() error          { _, err := w.userTask("restart", ""); return err }
func (w WindowsService) Status() (string, error) { return w.userTask("status", "") }

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

func (SystemdService) Install(string) error    { return Unsupported{Platform: "linux"}.Install("") }
func (SystemdService) Uninstall() error        { return Unsupported{Platform: "linux"}.Uninstall() }
func (SystemdService) Start() error            { return Unsupported{Platform: "linux"}.Start() }
func (SystemdService) Stop() error             { return Unsupported{Platform: "linux"}.Stop() }
func (SystemdService) Restart() error          { return Unsupported{Platform: "linux"}.Restart() }
func (SystemdService) Status() (string, error) { return Unsupported{Platform: "linux"}.Status() }
func (LaunchdService) Install(string) error    { return Unsupported{Platform: "darwin"}.Install("") }
func (LaunchdService) Uninstall() error        { return Unsupported{Platform: "darwin"}.Uninstall() }
func (LaunchdService) Start() error            { return Unsupported{Platform: "darwin"}.Start() }
func (LaunchdService) Stop() error             { return Unsupported{Platform: "darwin"}.Stop() }
func (LaunchdService) Restart() error          { return Unsupported{Platform: "darwin"}.Restart() }
func (LaunchdService) Status() (string, error) { return Unsupported{Platform: "darwin"}.Status() }

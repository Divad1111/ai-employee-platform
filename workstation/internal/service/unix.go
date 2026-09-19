//go:build !windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// WindowsService 非 Windows 占位。
type WindowsService struct{ Name string }

func (WindowsService) Install(string) error    { return Unsupported{Platform: "windows"}.Install("") }
func (WindowsService) Uninstall() error        { return Unsupported{Platform: "windows"}.Uninstall() }
func (WindowsService) Start() error            { return Unsupported{Platform: "windows"}.Start() }
func (WindowsService) Stop() error             { return Unsupported{Platform: "windows"}.Stop() }
func (WindowsService) Restart() error          { return Unsupported{Platform: "windows"}.Restart() }
func (WindowsService) Status() (string, error) { return Unsupported{Platform: "windows"}.Status() }

// SystemdService Linux 服务管理。
type SystemdService struct{ Unit string }

func (s SystemdService) isUser() bool {
	return os.Geteuid() != 0
}

func (s SystemdService) unitPath() string {
	if s.isUser() {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "systemd", "user", s.Unit)
	}
	return filepath.Join("/etc/systemd/system", s.Unit)
}

func (s SystemdService) Install(binPath string) error {
	if binPath == "" {
		var err error
		binPath, err = os.Executable()
		if err != nil {
			return err
		}
	}
	binPath, _ = filepath.Abs(binPath)
	p := s.unitPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("创建 systemd 目录失败: %w", err)
	}

	home, _ := os.UserHomeDir()
	dataDir := os.Getenv("AIE_DATA_DIR")
	if dataDir == "" && home != "" {
		dataDir = filepath.Join(home, ".aie")
	}

	content := fmt.Sprintf(`[Unit]
Description=AI Employee Workstation
After=network.target

[Service]
Type=simple
Environment="AIE_DATA_DIR=%s"
ExecStart=%s daemon
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
`, dataDir, binPath)

	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return fmt.Errorf("写入 systemd unit 失败: %w", err)
	}

	args := []string{"daemon-reload"}
	enableArgs := []string{"enable", "--now", s.Unit}
	if s.isUser() {
		args = []string{"--user", "daemon-reload"}
		enableArgs = []string{"--user", "enable", "--now", s.Unit}
	}
	_ = exec.Command("systemctl", args...).Run()
	if out, err := exec.Command("systemctl", enableArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable: %w (%s)", err, string(out))
	}
	if link, err := createGlobalSymlink(binPath); err == nil {
		fmt.Printf("已建立全局 PATH 软链接: %s -> %s\n", link, binPath)
	}
	return nil
}

func (s SystemdService) Uninstall() error {
	args := []string{"disable", "--now", s.Unit}
	if s.isUser() {
		args = []string{"--user", "disable", "--now", s.Unit}
	}
	_ = exec.Command("systemctl", args...).Run()
	_ = os.Remove(s.unitPath())
	reloadArgs := []string{"daemon-reload"}
	if s.isUser() {
		reloadArgs = []string{"--user", "daemon-reload"}
	}
	_ = exec.Command("systemctl", reloadArgs...).Run()
	removeGlobalSymlink()
	return nil
}

func (s SystemdService) Start() error {
	args := []string{"start", s.Unit}
	if s.isUser() {
		args = []string{"--user", "start", s.Unit}
	}
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl start: %w (%s)", err, string(out))
	}
	return nil
}

func (s SystemdService) Stop() error {
	args := []string{"stop", s.Unit}
	if s.isUser() {
		args = []string{"--user", "stop", s.Unit}
	}
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl stop: %w (%s)", err, string(out))
	}
	return nil
}

func (s SystemdService) Restart() error {
	args := []string{"restart", s.Unit}
	if s.isUser() {
		args = []string{"--user", "restart", s.Unit}
	}
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl restart: %w (%s)", err, string(out))
	}
	return nil
}

func (s SystemdService) Status() (string, error) {
	args := []string{"status", s.Unit}
	if s.isUser() {
		args = []string{"--user", "status", s.Unit}
	}
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	return string(out), err
}

// LaunchdService macOS launchd 服务管理（采用当前用户 ~/Library/LaunchAgents）。
type LaunchdService struct{ Label string }

func (l LaunchdService) plistPath() string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		return filepath.Join(home, "Library", "LaunchAgents", l.Label+".plist")
	}
	return filepath.Join("/Library/LaunchDaemons", l.Label+".plist")
}

func (l LaunchdService) Install(binPath string) error {
	if binPath == "" {
		var err error
		binPath, err = os.Executable()
		if err != nil {
			return err
		}
	}
	binPath, _ = filepath.Abs(binPath)
	p := l.plistPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("创建 LaunchAgents 目录失败: %w", err)
	}

	home, _ := os.UserHomeDir()
	dataDir := os.Getenv("AIE_DATA_DIR")
	if dataDir == "" && home != "" {
		dataDir = filepath.Join(home, ".aie")
	}
	_ = os.MkdirAll(dataDir, 0o755)
	logPath := filepath.Join(dataDir, "daemon.log")
	errPath := filepath.Join(dataDir, "daemon_err.log")
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		pathEnv = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	}

	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>daemon</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>AIE_DATA_DIR</key>
    <string>%s</string>
    <key>PATH</key>
    <string>%s</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, l.Label, binPath, dataDir, pathEnv, logPath, errPath)

	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return fmt.Errorf("写入 launchd plist 失败: %w", err)
	}

	// 自动加载并启动
	_ = exec.Command("launchctl", "unload", p).Run()
	if out, err := exec.Command("launchctl", "load", "-w", p).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %w (%s)", err, string(out))
	}
	if link, err := createGlobalSymlink(binPath); err == nil {
		fmt.Printf("已建立全局 PATH 软链接: %s -> %s\n", link, binPath)
	}
	return nil
}

func (l LaunchdService) Uninstall() error {
	p := l.plistPath()
	_ = exec.Command("launchctl", "unload", p).Run()
	_ = os.Remove(p)
	removeGlobalSymlink()
	return nil
}

func (l LaunchdService) Start() error {
	p := l.plistPath()
	out, err := exec.Command("launchctl", "load", "-w", p).CombinedOutput()
	if err != nil {
		if _, err2 := exec.Command("launchctl", "start", l.Label).CombinedOutput(); err2 == nil {
			return nil
		}
		return fmt.Errorf("launchctl start: %w (%s)", err, string(out))
	}
	return nil
}

func (l LaunchdService) Stop() error {
	p := l.plistPath()
	out, err := exec.Command("launchctl", "unload", p).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl stop/unload: %w (%s)", err, string(out))
	}
	return nil
}

func (l LaunchdService) Restart() error {
	_ = l.Stop()
	return l.Start()
}

func (l LaunchdService) Status() (string, error) {
	out, err := exec.Command("launchctl", "list", l.Label).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("服务未运行或未加载: %s\nplist=%s", string(out), l.plistPath()), err
	}
	return string(out), nil
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
	if realPath, err := filepath.EvalSymlinks(binPath); err == nil {
		binPath = realPath
	}

	home, _ := os.UserHomeDir()
	candidates := []string{
		"/opt/homebrew/bin/aew",
		"/usr/local/bin/aew",
	}
	if home != "" {
		userBin := filepath.Join(home, ".local", "bin")
		_ = os.MkdirAll(userBin, 0o755)
		candidates = append(candidates, filepath.Join(userBin, "aew"))
	}

	for _, target := range candidates {
		if target == binPath {
			continue
		}
		dir := filepath.Dir(target)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		_ = os.Remove(target)
		if err := os.Symlink(binPath, target); err == nil {
			return target, nil
		}
	}
	return "", fmt.Errorf("无法在系统 PATH 目录写入软链接")
}

func removeGlobalSymlink() {
	home, _ := os.UserHomeDir()
	targets := []string{
		"/opt/homebrew/bin/aew",
		"/usr/local/bin/aew",
	}
	if home != "" {
		targets = append(targets, filepath.Join(home, ".local", "bin", "aew"))
	}
	for _, t := range targets {
		_ = os.Remove(t)
	}
}

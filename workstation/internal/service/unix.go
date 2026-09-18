//go:build !windows

package service

import (
	"fmt"
	"os"
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

// SystemdService 写入 unit 文件骨架。
type SystemdService struct{ Unit string }

func (s SystemdService) unitPath() string {
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
	content := fmt.Sprintf(`[Unit]
Description=AI Employee Workstation
After=network.target

[Service]
Type=simple
ExecStart=%s daemon
Restart=on-failure

[Install]
WantedBy=multi-user.target
`, binPath)
	if err := os.WriteFile(s.unitPath(), []byte(content), 0o644); err != nil {
		return fmt.Errorf("写入 systemd unit 失败（可能需要 root）: %w", err)
	}
	return nil
}

func (s SystemdService) Uninstall() error { return os.Remove(s.unitPath()) }
func (s SystemdService) Start() error {
	return fmt.Errorf("请执行: systemctl start %s", s.Unit)
}
func (s SystemdService) Stop() error {
	return fmt.Errorf("请执行: systemctl stop %s", s.Unit)
}
func (s SystemdService) Restart() error {
	return fmt.Errorf("请执行: systemctl restart %s", s.Unit)
}
func (s SystemdService) Status() (string, error) {
	return fmt.Sprintf("unit=%s path=%s", s.Unit, s.unitPath()), nil
}

// LaunchdService macOS plist 骨架。
type LaunchdService struct{ Label string }

func (l LaunchdService) plistPath() string {
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
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array><string>%s</string><string>daemon</string></array>
  <key>RunAtLoad</key><true/>
</dict></plist>
`, l.Label, binPath)
	if err := os.WriteFile(l.plistPath(), []byte(content), 0o644); err != nil {
		return fmt.Errorf("写入 launchd plist 失败: %w", err)
	}
	return nil
}

func (l LaunchdService) Uninstall() error { return os.Remove(l.plistPath()) }
func (l LaunchdService) Start() error {
	return fmt.Errorf("请执行: launchctl load %s", l.plistPath())
}
func (l LaunchdService) Stop() error {
	return fmt.Errorf("请执行: launchctl unload %s", l.plistPath())
}
func (l LaunchdService) Restart() error { return l.Start() }
func (l LaunchdService) Status() (string, error) {
	return fmt.Sprintf("label=%s plist=%s", l.Label, l.plistPath()), nil
}

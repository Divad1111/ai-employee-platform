//go:build windows

package process

import "os"

// Windows 上 FindProcess 不验证存活；由上层 Recovery 结合其它信号判断。
func signalAlive(_ *os.Process) error {
	return nil
}

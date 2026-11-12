//go:build windows

package main

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
)

func checkWindowsService() (bool, error) {
	if len(os.Args) < 2 || (os.Args[1] != "daemon" && os.Args[1] != "service") {
		return false, nil
	}
	token, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return true, err
	}
	var session uint32
	if err = windows.ProcessIdToSessionId(uint32(os.Getpid()), &session); err != nil {
		return true, err
	}
	if session == 0 || token.User.Sid.IsWellKnown(windows.WinLocalSystemSid) {
		return true, errors.New("AEW 需要登录的 Windows 用户会话。请在该用户的终端运行 aew service install 和 aew service start")
	}
	return false, nil
}

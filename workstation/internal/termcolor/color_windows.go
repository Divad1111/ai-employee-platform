//go:build windows

package termcolor

import (
	"os"

	"golang.org/x/sys/windows"
)

func enableWindowsVT() bool {
	success := false
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		handle := windows.Handle(f.Fd())
		var mode uint32
		if err := windows.GetConsoleMode(handle, &mode); err == nil {
			if err := windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err == nil {
				success = true
			}
		}
	}

	// 尝试激活活动控制台缓冲区 CONOUT$
	conout, err := windows.CreateFile(
		windows.StringToUTF16Ptr("CONOUT$"),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err == nil {
		defer windows.CloseHandle(conout)
		var mode uint32
		if err := windows.GetConsoleMode(conout, &mode); err == nil {
			if err := windows.SetConsoleMode(conout, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err == nil {
				success = true
			}
		}
	}

	return success
}

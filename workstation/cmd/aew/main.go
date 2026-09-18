// Package main 是 Workstation 程序 aew 的入口。
// 同一二进制既可作为 CLI，也可作为 Daemon / 系统服务进程。
// 设计依据：设计文档 §40、§122–§123。
package main

import (
	"fmt"
	"os"

	"github.com/ai-employee-platform/workstation/internal/app"
)

// main 解析子命令并交给 app 层执行。
func main() {
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "aew 错误: %v\n", err)
		os.Exit(1)
	}
}

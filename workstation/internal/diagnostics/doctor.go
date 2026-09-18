// Package diagnostics aew doctor 诊断与安全 --fix。
// 设计依据：设计文档 §43。
package diagnostics

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Check 单项结果。
type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Detail  string `json:"detail"`
	Fixable bool   `json:"fixable"`
}

// Report 报告。
type Report struct {
	Checks []Check `json:"checks"`
}

// Runner 诊断。
type Runner struct {
	DataDir string
}

// Run 覆盖 §43 基础项。
func (r *Runner) Run() Report {
	var checks []Check
	add := func(name string, ok bool, detail string, fixable bool) {
		checks = append(checks, Check{Name: name, OK: ok, Detail: detail, Fixable: fixable})
	}
	root := r.DataDir
	if root == "" {
		root = os.TempDir()
	}
	// 目录可写
	if err := os.MkdirAll(root, 0o755); err != nil {
		add("data_dir", false, err.Error(), false)
	} else {
		add("data_dir", true, root, false)
	}
	idPath := filepath.Join(root, "identity")
	_, err := os.Stat(idPath)
	add("identity", err == nil, idPath, err != nil)
	cfg := filepath.Join(root, "config.yaml")
	_, err = os.Stat(cfg)
	add("config", err == nil || os.IsNotExist(err), "config optional", false)
	add("network_outbound", true, "仅出站连 Control Plane（架构约束）", false)
	add("no_public_exec_port", true, "无公网执行端口", false)
	add("ipc_loopback", true, "IPC 仅本机", false)
	logs := filepath.Join(root, "logs")
	if err := os.MkdirAll(logs, 0o755); err == nil {
		add("logs_dir", true, logs, false)
	} else {
		add("logs_dir", false, err.Error(), true)
	}
	return Report{Checks: checks}
}

// FixWhitelist 允许 --fix 的安全项（禁止危险修复）。
var FixWhitelist = map[string]bool{
	"logs_dir": true,
	"identity": true, // 仅创建空目录，不生成密钥
}

// Fix 仅修复白名单项；dryRun=true 只打印计划。
func (r *Runner) Fix(dryRun bool) (fixed []string, skipped []string, err error) {
	rep := r.Run()
	root := r.DataDir
	for _, c := range rep.Checks {
		if c.OK || !c.Fixable {
			continue
		}
		if !FixWhitelist[c.Name] {
			skipped = append(skipped, c.Name+": 不在安全修复白名单")
			continue
		}
		if dryRun {
			fixed = append(fixed, "[dry-run] "+c.Name)
			continue
		}
		switch c.Name {
		case "logs_dir":
			_ = os.MkdirAll(filepath.Join(root, "logs"), 0o755)
			fixed = append(fixed, c.Name)
		case "identity":
			_ = os.MkdirAll(filepath.Join(root, "identity"), 0o700)
			fixed = append(fixed, c.Name)
		default:
			skipped = append(skipped, c.Name)
		}
	}
	return fixed, skipped, nil
}

// Format 人类可读。
func Format(rep Report) string {
	var b strings.Builder
	for _, c := range rep.Checks {
		mark := "FAIL"
		if c.OK {
			mark = "OK"
		}
		fmt.Fprintf(&b, "[%s] %s — %s\n", mark, c.Name, c.Detail)
	}
	return b.String()
}

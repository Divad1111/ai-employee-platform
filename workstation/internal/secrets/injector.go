// Package secrets 本地运行时 Secret 注入（环境变量），禁止把明文写入普通日志。
// 设计依据：设计文档 §62、§88。
package secrets

import (
	"fmt"
	"strings"
)

// Ref 来自 Control Plane 的已解析引用（明文仅内存短暂持有）。
type Ref struct {
	EnvKey   string
	SecretID string
	Value    string // 禁止 fmt/log
}

// BuildEnv 生成进程环境变量；不记录 Value。
func BuildEnv(base []string, refs []Ref) ([]string, error) {
	out := append([]string{}, base...)
	for _, r := range refs {
		if r.EnvKey == "" {
			return nil, fmt.Errorf("secret %s 缺少 env key", r.SecretID)
		}
		if r.Value == "" {
			return nil, fmt.Errorf("secret %s 解析失败", r.SecretID)
		}
		out = append(out, r.EnvKey+"="+r.Value)
	}
	return out, nil
}

// SanitizeError 确保错误字符串不含疑似密钥。
func SanitizeError(err error, secrets []string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, s := range secrets {
		if s != "" && strings.Contains(msg, s) {
			msg = strings.ReplaceAll(msg, s, "***")
		}
	}
	return fmt.Errorf("%s", msg)
}

// DescribeRefs 仅描述 ID/EnvKey（无明文），用于诊断。
func DescribeRefs(refs []Ref) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, fmt.Sprintf("%s→%s", r.SecretID, r.EnvKey))
	}
	return out
}

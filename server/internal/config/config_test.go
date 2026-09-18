// Package config 的单元测试。
package config

import "testing"

// TestLoadDefaults 验证未设置环境变量时使用开发默认值。
func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.Env != "development" {
		t.Fatalf("期望 Env=development，实际=%s", cfg.Env)
	}
	if cfg.HTTPAddr == "" || cfg.GRPCAddr == "" {
		t.Fatal("HTTP/gRPC 地址不应为空")
	}
}

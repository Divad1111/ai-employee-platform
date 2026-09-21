// Package config 负责 Workstation 本地配置。
// 敏感信息不写入 YAML；身份材料存放于 identity 目录。
// 设计依据：设计文档 §80、§124、§45–§48。
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Version 为 aew 构建版本号。
const Version = "0.0.1-dev"

// Config 表示 Workstation 有效配置。
type Config struct {
	Workstation struct {
		Name string `yaml:"name"`
	} `yaml:"workstation"`
	ControlPlane struct {
		Endpoint string `yaml:"endpoint"`
	} `yaml:"control_plane"`
	Runtime struct {
		MaxSessions    int `yaml:"max_sessions"`
		IdleTimeoutSec int `yaml:"idle_timeout"`
	} `yaml:"runtime"`
	Providers map[string]ProviderCfg `yaml:"providers"`
	Security  struct {
		RequireMTLS bool `yaml:"require_mtls"`
	} `yaml:"security"`
	// 解析后的便捷字段
	Name                 string
	ControlPlaneEndpoint string
	MaxSessions          int
	IdleTimeoutSec       int
}

// ProviderCfg 单个 Provider 配置（无密钥）。
type ProviderCfg struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"` // 本机可执行文件路径覆盖
}

// Default 返回开发默认配置。
func Default() *Config {
	c := &Config{}
	c.Workstation.Name = "local-dev"
	c.ControlPlane.Endpoint = "localhost:9090"
	c.Runtime.MaxSessions = 1
	c.Runtime.IdleTimeoutSec = 600
	c.Providers = map[string]ProviderCfg{
		"cursor":      {Enabled: true},
		"codex":       {Enabled: true},
		"antigravity": {Enabled: true},
	}
	c.Security.RequireMTLS = true
	c.normalize()
	return c
}

func (c *Config) normalize() {
	c.Name = c.Workstation.Name
	c.ControlPlaneEndpoint = c.ControlPlane.Endpoint
	c.MaxSessions = c.Runtime.MaxSessions
	if c.MaxSessions != 1 {
		// Q-03：V1 强制 1
		c.MaxSessions = 1
	}
	c.IdleTimeoutSec = c.Runtime.IdleTimeoutSec
	if c.IdleTimeoutSec <= 0 {
		c.IdleTimeoutSec = 600
	}
	if c.ControlPlaneEndpoint == "" {
		c.ControlPlaneEndpoint = "localhost:9090"
	}
}

// LoadFile 从 YAML 加载；文件不存在则返回 Default。
func LoadFile(path string) (*Config, error) {
	c := Default()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	c.normalize()
	return c, nil
}

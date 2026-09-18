// Package config 负责 Control Plane 配置加载。
// 采用「默认配置 + 环境覆盖」策略，禁止直接改默认文件。
// 设计依据：设计文档 §80。
package config

import "os"

// Version 为 Control Plane 构建版本号（后续可由 ldflags 注入）。
const Version = "0.0.1-dev"

// Config 表示运行时有效配置。
type Config struct {
	// Env 运行环境：development / staging / production
	Env string
	// HTTPAddr Admin / Feishu Webhook 等 HTTP 监听地址
	HTTPAddr string
	// GRPCAddr Workstation gRPC（mTLS）监听地址
	GRPCAddr string
	// DatabaseURL PostgreSQL 连接串（Secret 勿写入仓库）
	DatabaseURL string
}

// Load 从环境变量加载配置，缺省使用开发默认值。
func Load() (*Config, error) {
	cfg := &Config{
		Env:         getenv("AIE_ENV", "development"),
		HTTPAddr:    getenv("AIE_HTTP_ADDR", ":8080"),
		GRPCAddr:    getenv("AIE_GRPC_ADDR", ":9090"),
		DatabaseURL: getenv("AIE_DATABASE_URL", "postgres://aie:aie@localhost:5432/aie?sslmode=disable"),
	}
	return cfg, nil
}

// getenv 读取环境变量，空则返回默认值。
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

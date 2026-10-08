package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)


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
	// PublicHTTPPort 宿主机/外部访问的 HTTP 端口（优先取 AIE_HTTP_PORT，缺省从 HTTPAddr 提取）
	PublicHTTPPort string
	// PublicGRPCPort 宿主机/外部访问的 gRPC 端口（优先取 AIE_GRPC_PORT）
	PublicGRPCPort string
	// PublicServerURL 外部完整访问地址（如 http://192.168.1.100:9080，可选）
	PublicServerURL string
}

// Load 从环境变量加载配置，缺省使用开发默认值。
func Load() (*Config, error) {
	cfg := &Config{
		Env:         getenv("AIE_ENV", "development"),
		HTTPAddr:    getenv("AIE_HTTP_ADDR", ":8080"),
		GRPCAddr:    getenv("AIE_GRPC_ADDR", ":9090"),
		DatabaseURL: LoadDatabaseDSN(),
	}
	httpPort := getenv("AIE_HTTP_PORT", "")
	if httpPort == "" {
		if _, p, err := net.SplitHostPort(cfg.HTTPAddr); err == nil {
			httpPort = p
		} else {
			httpPort = strings.TrimPrefix(cfg.HTTPAddr, ":")
		}
	}
	cfg.PublicHTTPPort = httpPort
	cfg.PublicGRPCPort = getenv("AIE_GRPC_PORT", "9090")
	cfg.PublicServerURL = getenv("AIE_PUBLIC_SERVER_URL", "")
	return cfg, nil
}


// LoadDatabaseDSN 加载 PostgreSQL DSN。
// 优先使用 AIE_DATABASE_URL；若未设置，则根据 POSTGRES_* 环境变量进行安全 URL 编码组装，
// 避免密码中包含 /、@、:、?、# 等特殊字符导致解析失败。
func LoadDatabaseDSN() string {
	if raw := os.Getenv("AIE_DATABASE_URL"); raw != "" {
		return raw
	}
	user := getenv("POSTGRES_USER", "aie")
	pass := getenv("POSTGRES_PASSWORD", "aie")
	host := getenv("POSTGRES_HOST", "localhost")
	port := getenv("POSTGRES_PORT", "5432")
	dbName := getenv("POSTGRES_DB", "aie")
	sslMode := getenv("POSTGRES_SSLMODE", "disable")

	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, pass),
		Host:     fmt.Sprintf("%s:%s", host, port),
		Path:     "/" + dbName,
		RawQuery: "sslmode=" + sslMode,
	}
	return u.String()
}

// getenv 读取环境变量，空则返回默认值。
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}


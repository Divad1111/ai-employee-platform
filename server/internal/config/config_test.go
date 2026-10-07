// Package config 的单元测试。
package config

import (
	"database/sql"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)


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

func TestPgxConnString(t *testing.T) {
	rawURL := "postgres://qixia:qixia,./123@postgres:5432/aie?sslmode=disable"
	_, err := pgx.ParseConfig(rawURL)
	if err == nil {
		t.Errorf("expected rawURL to fail, but succeeded")
	} else {
		t.Logf("rawURL error as expected: %v", err)
	}

	// 1. Keyword-value format with quotes
	kvDSN := "host=postgres port=5432 user=qixia password='qixia,./123' dbname=aie sslmode=disable"
	cfgKV, err := pgx.ParseConfig(kvDSN)
	if err != nil {
		t.Errorf("kvDSN failed: %v", err)
	} else if cfgKV.Password != "qixia,./123" {
		t.Errorf("expected password 'qixia,./123', got '%s'", cfgKV.Password)
	}

	complexDSN := "host=postgres port=5432 user=qixia password='P@ssw0rd:123/abc?def#ghi' dbname=aie sslmode=disable"
	cfgComplex, err := pgx.ParseConfig(complexDSN)
	if err != nil {
		t.Errorf("complexDSN failed: %v", err)
	} else if cfgComplex.Password != "P@ssw0rd:123/abc?def#ghi" {
		t.Errorf("expected complex password, got '%s'", cfgComplex.Password)
	}

	db, err := sql.Open("pgx", kvDSN)
	if err != nil {
		t.Errorf("sql.Open failed: %v", err)
	}
	_ = db.Close()



	// 3. url.URL with UserPassword
	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword("qixia", "qixia,./123"),
		Host:     "postgres:5432",
		Path:     "/aie",
		RawQuery: "sslmode=disable",
	}
	urlStr := u.String()
	t.Logf("Generated URL: %s", urlStr)
	cfgGenerated, err := pgx.ParseConfig(urlStr)
	if err != nil {
		t.Errorf("cfgGenerated failed: %v", err)
		t.Logf("cfgGenerated parsed successfully: user=%s, password=%s, host=%s, db=%s",
			cfgGenerated.User, cfgGenerated.Password, cfgGenerated.Host, cfgGenerated.Database)
	}
}


func TestLoadDatabaseDSN(t *testing.T) {
	t.Setenv("AIE_DATABASE_URL", "")
	t.Setenv("POSTGRES_USER", "qixia")
	t.Setenv("POSTGRES_PASSWORD", "qixia,./123")
	t.Setenv("POSTGRES_HOST", "postgres")
	t.Setenv("POSTGRES_PORT", "5432")
	t.Setenv("POSTGRES_DB", "aie")

	dsn := LoadDatabaseDSN()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("LoadDatabaseDSN returned unparseable DSN: %v", err)
	}
	if cfg.User != "qixia" || cfg.Password != "qixia,./123" {
		t.Fatalf("LoadDatabaseDSN credentials mismatch: user=%s, pass=%s", cfg.User, cfg.Password)
	}
}




// Package store 封装 Workstation 本地 SQLite。
// ACK 语义要求关键命令在 SQLite COMMIT 成功后才确认。
// 设计依据：设计文档 §22、§25。
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sync"
)

//go:embed schema.sql
var schemaFS embed.FS

// 默认驱动名；可通过 RegisterDriver 覆盖（例如 modernc.org/sqlite 注册为 "sqlite"）。
var (
	driverMu   sync.RWMutex
	driverName = "sqlite"
)

// RegisterDriver 设置 sql.Open 使用的驱动名（须已在 init 中注册）。
func RegisterDriver(name string) {
	driverMu.Lock()
	defer driverMu.Unlock()
	driverName = name
}

func currentDriver() string {
	driverMu.RLock()
	defer driverMu.RUnlock()
	return driverName
}

// Store 本地持久化门面。
type Store struct {
	db *sql.DB
}

// Open 打开（或创建）SQLite 数据库并执行幂等 schema 迁移。
// 调用方需确保已 blank-import 对应驱动，例如：
//
//	import _ "modernc.org/sqlite"
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open(currentDriver(), path)
	if err != nil {
		return nil, fmt.Errorf("打开 sqlite 失败: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// SchemaSQL 返回嵌入的 schema 文本（测试与工具可用）。
func SchemaSQL() (string, error) {
	b, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Migrate 执行嵌入的 schema.sql（可重复执行）。
func (s *Store) Migrate(ctx context.Context) error {
	sqlText, err := SchemaSQL()
	if err != nil {
		return fmt.Errorf("读取 schema 失败: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, sqlText); err != nil {
		return fmt.Errorf("执行 schema 失败: %w", err)
	}
	return nil
}

// DB 返回底层 *sql.DB。
func (s *Store) DB() *sql.DB {
	return s.db
}

// Close 关闭数据库。
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

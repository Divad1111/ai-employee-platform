// Package store 的单元测试（不依赖外部 SQLite 驱动下载）。
package store

import (
	"strings"
	"testing"
)

// TestSchemaContainsRequiredTables 校验 schema 覆盖设计文档 §25 关键表。
func TestSchemaContainsRequiredTables(t *testing.T) {
	sqlText, err := SchemaSQL()
	if err != nil {
		t.Fatalf("读取 schema 失败: %v", err)
	}
	required := []string{
		"workstation_state",
		"employees",
		"workspaces",
		"sessions",
		"jobs",
		"job_events",
		"server_commands",
		"worker_events",
		"outbox",
		"provider_installations",
		"updates",
	}
	for _, table := range required {
		needle := "CREATE TABLE IF NOT EXISTS " + table
		if !strings.Contains(sqlText, needle) {
			t.Fatalf("schema 缺少表定义: %s", table)
		}
	}
	// ACK / sequence 语义字段
	for _, col := range []string{"sequence", "acked", "event_id"} {
		if !strings.Contains(sqlText, col) {
			t.Fatalf("schema 缺少关键列: %s", col)
		}
	}
}

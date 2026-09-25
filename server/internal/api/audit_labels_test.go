package api

import (
	"strings"
	"testing"
)

func TestAuditLabelsChinese(t *testing.T) {
	if actionLabelCN("role.update") != "修改角色权限" {
		t.Fatal(actionLabelCN("role.update"))
	}
	if resultLabelCN("success") != "成功" {
		t.Fatal(resultLabelCN("success"))
	}
	text := applyReplacements("角色 ADMIN 的权限 job.write：范围 OWN → ALL；USER_BONUS u-1", []replacePair{
		{from: "USER_BONUS", to: "用户额外"},
		{from: "job.write", to: "创建或推进任务"},
		{from: "ADMIN", to: "管理员"},
		{from: "OWN", to: "仅本人"},
		{from: "ALL", to: "全部资源"},
		{from: "u-1", to: "ada"},
	})
	want := "角色 管理员 的权限 创建或推进任务：范围 仅本人 → 全部资源；用户额外 ada"
	if text != want {
		t.Fatalf("got %s", text)
	}
}

func TestAuditLabelsFeishuClientName(t *testing.T) {
	summary := fallbackAuditSummary(map[string]string{
		"prompt":      "你好",
		"source":      "feishu",
		"client_name": "张三",
	})
	if !strings.Contains(summary, "客户端：张三") || !strings.Contains(summary, "提示词：你好") {
		t.Fatalf("fallbackAuditSummary 缺少客户端或提示词: %s", summary)
	}
}


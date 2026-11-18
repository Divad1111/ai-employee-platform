package collector

import "strings"

// This reports returned OAuth scopes, not a claim about a resource's ACL.
// Manual tokens have no trustworthy scope introspection and are marked unknown.
func capabilityStatus(p Profile, c Credentials) []map[string]any {
	groups := []struct{ name, scopes string }{
		{"云文档读取", "docx:document:readonly docx:document"},
		{"云文档编辑", "docx:document"},
		{"内容搜索", "search:docs:read"},
		{"云空间读取", "drive:drive:readonly drive:drive"},
		{"知识库", "wiki:wiki:readonly wiki:wiki"},
		{"电子表格", "sheets:spreadsheet:readonly sheets:spreadsheet"},
		{"多维表格", "bitable:app:readonly bitable:app"},
		{"日历", "calendar:calendar:readonly calendar:calendar"},
		{"任务", "task:task:readonly task:task"},
		{"消息", "im:message:readonly im:message im:message:get_as_user"},
	}
	granted := strings.Fields(p.GrantedScopes)
	out := []map[string]any{}
	for _, group := range groups {
		status := "not_granted"
		if c.AccessToken == "" {
			status = "disconnected"
		} else if p.GrantedScopes == "" {
			status = "unknown"
		} else if matchesAny(granted, strings.Fields(group.scopes)) {
			status = "granted"
		}
		out = append(out, map[string]any{"name": group.name, "status": status})
	}
	return out
}

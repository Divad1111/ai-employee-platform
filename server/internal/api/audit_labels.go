package api

import (
	"context"
	"sort"
	"strings"

	"github.com/ai-employee-platform/server/internal/audit"
)

func actionLabelCN(action string) string {
	labels := map[string]string{
		"login":                       "登录",
		"logout":                      "退出登录",
		"auth.step_up":                "二次鉴权",
		"system.bootstrap":            "系统初始化",
		"user.create":                 "新建用户",
		"user.update":                 "修改用户",
		"user.delete":                 "删除用户",
		"user.enable":                 "启用用户",
		"user.disable":                "停用用户",
		"role.create":                 "新建角色",
		"role.update":                 "修改角色权限",
		"role.delete":                 "删除角色",
		"quota.upsert":                "调整配额",
		"quota.delete":                "删除配额",
		"employee.create":             "新建数字员工",
		"employee.update":             "修改数字员工",
		"employee.delete":             "删除数字员工",
		"workspace.create":            "新建工作区",
		"workspace.update":            "修改工作区",
		"workspace.delete":            "删除工作区",
		"workspace.bind":              "绑定工作区",
		"workstation.update":          "修改工作站",
		"workstation.delete":          "删除工作站",
		"workstation.member.add":      "添加工作站成员",
		"workstation.member.remove":   "移除工作站成员",
		"workstation.cert.revoke":     "吊销工作站证书",
		"enrollment.create_token":     "签发接入令牌",
		"enrollment.enroll":           "工作站注册",
		"job.create":                  "创建任务",
		"job.transition":              "任务状态变更",
		"job.execute":                 "执行任务",
		"session.create":              "创建会话",
		"session.transition":          "会话状态变更",
		"message.send":                "发送消息",
		"secret.put":                  "写入机密",
		"secret.access":               "读取机密",
		"secret.bind":                 "绑定机密",
		"secret.rotate":               "轮换机密",
		"secret.delete":               "删除机密",
		"approval.create":             "发起审批",
		"approval.approve":            "批准审批",
		"approval.reject":             "驳回审批",
		"approval.totp":               "审批动态口令",
		"approval.critical_deny":      "关键操作被拒绝",
		"totp.enroll":                 "绑定动态口令",
		"totp.re_enroll":              "重新绑定动态口令",
		"totp.enroll_pending":         "动态口令待确认",
		"totp.confirm":                "确认动态口令",
		"totp.activate":               "启用动态口令",
		"totp.disable":                "关闭动态口令",
		"feishu.config":               "修改飞书配置",
		"feishu.test_message":         "发送飞书测试消息",
		"feishu.binding.upsert":       "保存飞书别名绑定",
		"feishu.binding.delete":       "删除飞书别名绑定",
		"workflow.upsert":             "保存工作流",
		"workflow.delete":             "删除工作流",
		"workflow.grant":              "授予工作流",
		"workflow.revoke":             "收回工作流",
		"workflow.import":             "导入工作流",
		"skill.upsert":                "保存技能",
		"skill.delete":                "删除技能",
		"skill.sync":                  "同步技能",
		"knowledge.upsert":            "保存知识",
		"knowledge.delete":            "删除知识",
		"knowledge.reindex":           "重建知识索引",
		"mcp.token.issue":             "签发 MCP 令牌",
		"mcp.token.revoke":            "吊销 MCP 令牌",
		"provider.upsert":             "保存模型供应商",
		"provider.version.add":        "新增供应商版本",
		"artifact.upload":             "上传产物",
		"automation.create":           "新建自动化",
		"automation.update":           "修改自动化",
		"automation.delete":           "删除自动化",
		"automation.calendar_replace": "更新自动化日历",
		"automation.rotate_secrets":   "轮换自动化机密",
		"automation.webhook_denied":   "拒绝自动化回调",
		"permission.decide":           "权限判定",
		"permission.profile.upsert":   "保存权限配置",
		"permission.rule.upsert":      "保存权限规则",
		"audit.archive":               "归档审计",
	}
	if cn, ok := labels[action]; ok {
		return cn
	}
	return action
}

func resultLabelCN(result string) string {
	labels := map[string]string{
		"success":        "成功",
		"failed":         "失败",
		"fail":           "失败",
		"denied":         "拒绝",
		"deny":           "拒绝",
		"pending":        "待处理",
		"allow":          "允许",
		"ok":             "成功",
		"rate_limited":   "请求过于频繁",
		"disabled":       "账号已停用",
		"locked":         "账号已锁定",
		"not_configured": "未配置",
		"failed_token":   "令牌无效",
		"failed_sign":    "签名校验失败",
	}
	if cn, ok := labels[strings.ToLower(strings.TrimSpace(result))]; ok {
		return cn
	}
	return result
}

func actorTypeLabelCN(actorType string) string {
	switch strings.ToUpper(actorType) {
	case "USER":
		return "用户"
	case "SYSTEM":
		return "系统"
	case "WORKSTATION":
		return "工作站"
	case "EMPLOYEE":
		return "数字员工"
	default:
		if actorType == "" {
			return ""
		}
		return actorType
	}
}

func permLabelCN(code string) string {
	labels := map[string]string{
		"employee.read":     "查看数字员工",
		"employee.write":    "编辑数字员工",
		"employee.delete":   "删除数字员工",
		"workstation.read":  "查看工作站",
		"workstation.write": "编辑工作站",
		"job.read":          "查看任务",
		"job.write":         "创建或推进任务",
		"job.cancel":        "取消任务",
		"approval.read":     "查看审批",
		"approval.approve":  "审批决定",
		"secret.read":       "查看机密",
		"secret.write":      "编辑机密",
		"system.read":       "查看系统配置",
		"system.write":      "修改系统配置",
		"workspace.read":    "查看工作区",
		"workspace.write":   "编辑工作区",
		"session.read":      "查看会话",
		"session.write":     "编辑会话",
		"message.read":      "查看消息",
		"message.write":     "发送消息",
		"audit.read":        "查看审计",
		"enrollment.write":  "签发工作站接入令牌",
		"workflow.read":     "查看工作流、技能与知识",
		"workflow.write":    "编辑工作流、技能与知识",
		"workflow.delete":   "删除工作流、技能与知识",
		"workflow.grant":    "向员工授权工作流并签发令牌",
		"quota.read":        "查看配额",
		"quota.update":      "调整配额",
		"user.create":       "新建用户",
		"user.update":       "修改用户",
		"user.delete":       "删除用户",
		"user.disable":      "停用用户",
		"role.create":       "新建角色",
		"role.update":       "修改角色",
		"role.delete":       "删除角色",
		"automation.write":  "编辑自动化",
	}
	if cn, ok := labels[code]; ok {
		return cn
	}
	return code
}

func statusLabelCN(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "ACTIVE":
		return "启用"
	case "DISABLED":
		return "停用"
	case "STOPPED":
		return "已停止"
	case "DELETED":
		return "已删除"
	default:
		return status
	}
}

func resourceTypeLabelCN(t string) string {
	switch strings.ToUpper(t) {
	case "USER_BONUS":
		return "用户额外"
	case "USER":
		return "用户例外"
	case "ROLE":
		return "角色预设"
	case "WORKSTATION":
		return "工作站"
	case "DIGITAL_EMPLOYEE":
		return "数字员工"
	case "MONTHLY":
		return "每月"
	default:
		return t
	}
}

type replacePair struct {
	from string
	to   string
}

func applyReplacements(text string, pairs []replacePair) string {
	if text == "" {
		return ""
	}
	for _, p := range pairs {
		if p.from == "" || p.from == p.to {
			continue
		}
		text = strings.ReplaceAll(text, p.from, p.to)
	}
	return text
}

func (d Deps) auditReplacePairs(ctx context.Context) []replacePair {
	var pairs []replacePair
	add := func(from, to string) {
		from = strings.TrimSpace(from)
		to = strings.TrimSpace(to)
		if from == "" || to == "" || from == to {
			return
		}
		pairs = append(pairs, replacePair{from: from, to: to})
	}
	if d.Auth != nil {
		if users, err := d.Auth.Users().List(ctx); err == nil {
			for _, u := range users {
				if u == nil {
					continue
				}
				add(u.ID, u.Username)
			}
		}
		if roles, err := d.Auth.Users().ListRoles(ctx); err == nil {
			for _, role := range roles {
				label := roleLabelCN([]string{role.Name})
				if label == role.Name {
					if desc := strings.TrimSpace(role.Description); desc != "" {
						label = desc
					}
				}
				add(role.Name, label)
			}
		}
		if perms, err := d.Auth.Users().ListAllPermissions(ctx); err == nil {
			for _, p := range perms {
				add(p.Code, permLabelCN(p.Code))
			}
		}
	}
	if d.Employees != nil {
		if emps, err := d.Employees.List(ctx); err == nil {
			for _, e := range emps {
				if e != nil {
					add(e.ID, e.Name)
				}
			}
		}
	}
	if d.Workstations != nil && d.Workstations.Meta != nil {
		for _, id := range d.Workstations.Meta.ListIDs(ctx) {
			if name := d.Workstations.Meta.GetName(ctx, id); name != "" {
				add(id, name)
			}
		}
	}
	if d.Feishu != nil {
		for _, b := range d.Feishu.ListBindings() {
			if b.FeishuOpenID != "" {
				if name := d.Feishu.ResolveUserName(ctx, b.FeishuOpenID); name != "" {
					add(b.FeishuOpenID, name)
				}
			}
		}
	}
	for _, code := range []string{
		"USER_BONUS", "DIGITAL_EMPLOYEE", "WORKSTATION", "ROLE", "USER", "MONTHLY",
		"ACTIVE", "DISABLED", "STOPPED", "DELETED",
		"ALL", "OWN", "ASSIGNED", "NONE",
	} {
		switch code {
		case "ALL":
			add(code, "全部资源")
		case "OWN":
			add(code, "仅本人")
		case "ASSIGNED":
			add(code, "已分配")
		case "NONE":
			add(code, "无权限")
		default:
			if cn := resourceTypeLabelCN(code); cn != code {
				add(code, cn)
			}
			if cn := statusLabelCN(code); cn != code {
				add(code, cn)
			}
		}
	}
	for code, cn := range map[string]string{
		"employee.read": "查看数字员工", "job.write": "创建或推进任务",
		"feishu": "飞书", "cron": "定时任务", "calendar": "日历任务", "webhook": "Webhook",
		"web": "控制台", "api": "API", "system": "系统",
	} {
		add(code, cn)
	}
	// 已知权限码全部纳入，避免目录未加载时仍显示英文码
	for _, code := range []string{
		"employee.read", "employee.write", "employee.delete",
		"workstation.read", "workstation.write", "job.read", "job.write", "job.cancel",
		"approval.read", "approval.approve", "secret.read", "secret.write",
		"system.read", "system.write", "workspace.read", "workspace.write",
		"session.read", "session.write", "message.read", "message.write",
		"audit.read", "enrollment.write", "workflow.read", "workflow.write",
		"workflow.delete", "workflow.grant", "quota.read", "quota.update",
		"user.create", "user.read", "user.update", "user.delete", "user.disable",
		"role.create", "role.update", "role.delete", "automation.write",
	} {
		add(code, permLabelCN(code))
	}
	sort.Slice(pairs, func(i, j int) bool {
		return len(pairs[i].from) > len(pairs[j].from)
	})
	return pairs
}

func (d Deps) localizeAudit(ctx context.Context, items []audit.Entry) {
	pairs := d.auditReplacePairs(ctx)
	for i := range items {
		if items[i].Metadata == nil {
			items[i].Metadata = map[string]string{}
		}
		m := items[i].Metadata
		for k, v := range m {
			if k == "action_label" || k == "result_label" || k == "actor_type_label" {
				continue
			}
			m[k] = applyReplacements(v, pairs)
		}
		m["action_label"] = actionLabelCN(items[i].Action)
		m["result_label"] = resultLabelCN(items[i].Result)
		m["actor_type_label"] = actorTypeLabelCN(items[i].ActorType)
		if m["summary"] == "" {
			m["summary"] = fallbackAuditSummary(m)
		}
	}
}

func fallbackAuditSummary(m map[string]string) string {
	skip := map[string]bool{
		"actor_name": true, "username": true, "action_label": true,
		"result_label": true, "actor_type_label": true,
	}
	labels := map[string]string{
		"role": "角色", "permission": "权限", "id": "对象", "name": "名称",
		"alias": "别名", "employee_id": "数字员工", "resource_type": "类型",
		"resource_id": "对象", "token_limit": "Token 限额", "before": "原值",
		"after": "新值", "workstation_id": "工作站", "user_id": "用户",
		"prompt": "提示词", "source": "来源", "status": "状态",
		"client_name": "客户端",
	}
	var parts []string
	for k, v := range m {
		if skip[k] || strings.TrimSpace(v) == "" {
			continue
		}
		label := labels[k]
		if label == "" {
			continue
		}
		parts = append(parts, label+"："+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, "；")
}

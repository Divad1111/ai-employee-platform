package auth

import (
	"context"
	"errors"
	"strings"
)

var ErrReauthLimited = errors.New("二次认证尝试过多，请稍后重试")

// RequiresMFA 根据有效管理权限判断，避免自定义角色绕过强制 MFA。
func (s *Service) RequiresMFA(ctx context.Context, roles []string) bool {
	if !s.EnforceAdminMFA {
		return false
	}
	if AdminRoles(roles) {
		return true
	}
	grants, err := s.users.ListPermissionGrants(ctx, roles)
	if err != nil {
		return true
	}
	for _, g := range grants {
		if g.Scope == "NONE" {
			continue
		}
		_, action, _ := strings.Cut(g.Code, ".")
		mutation := action == "write" || action == "create" || action == "update" || action == "delete" || action == "manage" || action == "grant" || action == "restore" || action == "approve" || action == "deploy" || action == "console"
		if (g.Scope == "ALL" && mutation) || g.Code == "*" || strings.HasPrefix(g.Code, "infra.") || strings.HasPrefix(g.Code, "backup.") || strings.HasPrefix(g.Code, "secret.") || strings.HasPrefix(g.Code, "role.") || strings.HasPrefix(g.Code, "system.") || strings.HasPrefix(g.Code, "enrollment.") || strings.HasPrefix(g.Code, "permission.") || g.Code == "approval.approve" || (strings.HasPrefix(g.Code, "user.") && g.Code != "user.read") {
			return true
		}
	}
	return false
}

// AllowReauthentication 在执行密码哈希前同时限制来源和会话。
func (s *Service) AllowReauthentication(user, ip string) bool {
	return s.limiter.Allow("reauth:ip:"+ip) && s.limiter.Allow("reauth:request:"+user)
}

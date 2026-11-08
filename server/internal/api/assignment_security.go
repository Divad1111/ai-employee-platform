package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/job"
)

// AuthorizeAssignment 在任务真正出队时读取当前账号、角色和工作站授权。
// 自动化/飞书任务使用员工所有者作为委托主体，不能依靠过期的创建时授权。
func (d Deps) AuthorizeAssignment(ctx context.Context, j *job.Job, e *employee.Employee, node string) error {
	if d.Auth == nil || j == nil || e == nil || node == "" {
		return auth.ErrForbidden
	}
	// Provider 与守护进程使用同一 OS 账号，目录校验不能构成租户沙箱。
	// 生产环境因此禁止把同一工作站的执行账号共享给不同资源所有者。
	if d.Config != nil && strings.EqualFold(d.Config.Env, "production") {
		owner := d.workstationCreatedBy(ctx, node)
		if owner == "" || e.OwnerUserID == "" || e.OwnerUserID != owner {
			return fmt.Errorf("生产工作站只能运行其创建者名下的员工；不同用户请使用独立工作站与 OS 账号或虚拟机")
		}
	}
	uid := j.CreatedBy
	if uid == "" || strings.HasPrefix(uid, "automation:") || strings.HasPrefix(uid, "feishu:") || uid == "feishu" {
		uid = e.OwnerUserID
	}
	u, err := d.Auth.Users().FindByID(ctx, uid)
	if err != nil || u == nil || u.Status != auth.StatusActive {
		return auth.ErrForbidden
	}
	s := &auth.Session{UserID: u.ID, Roles: u.Roles}
	if d.Auth.AuthorizeAll(ctx, s, "job.write") == nil {
		return nil
	}
	if d.Auth.Authorize(ctx, s, "job.write") != nil || e.OwnerUserID != uid {
		return auth.ErrForbidden
	}
	if d.userIDMayUseWorkstation(ctx, uid, node, u.Roles) {
		return nil
	}
	return auth.ErrForbidden
}

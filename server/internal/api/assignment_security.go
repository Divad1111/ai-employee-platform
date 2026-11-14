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
	// 非公用生产工作站保持创建者专用；显式设为公用的工作站按下方当前角色/成员授权派发。
	// 不能在这里一律要求员工与工作站同属一个创建者，否则公用授权永远无法生效。
	if d.Config != nil && strings.EqualFold(d.Config.Env, "production") {
		if d.WSShare == nil {
			return auth.ErrForbidden
		}
		share, err := d.WSShare.Get(ctx, node)
		if err != nil || share.CreatedBy == "" || e.OwnerUserID == "" {
			return auth.ErrForbidden
		}
		if !share.IsPublic && e.OwnerUserID != share.CreatedBy {
			return fmt.Errorf("非公用工作站只能运行其创建者名下的员工；请将工作站设为公用并配置角色授权")
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

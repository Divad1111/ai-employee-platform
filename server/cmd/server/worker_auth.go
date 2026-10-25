package main

import (
	"context"
	"encoding/json"
	"fmt"
	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/session"
	"strings"
)

func resourceEventValidator(jobs *job.Service, sessions *session.Service, employees *employee.Service) func(string, *aiev1.Event) error {
	return func(ws string, ev *aiev1.Event) error {
		ctx := context.Background()
		var p map[string]string
		if ev.PayloadJson != "" {
			if err := json.Unmarshal([]byte(ev.PayloadJson), &p); err != nil {
				return err
			}
		}
		if ev.JobId == "" {
			ev.JobId = p["job_id"]
		} else if p["job_id"] != "" && p["job_id"] != ev.JobId {
			return fmt.Errorf("任务 ID 不一致")
		}
		if ev.EmployeeId == "" {
			ev.EmployeeId = p["employee_id"]
		} else if p["employee_id"] != "" && p["employee_id"] != ev.EmployeeId {
			return fmt.Errorf("员工 ID 不一致")
		}
		if ev.SessionId == "" {
			ev.SessionId = p["session_id"]
		} else if p["session_id"] != "" && p["session_id"] != ev.SessionId {
			return fmt.Errorf("会话 ID 不一致")
		}
		if strings.HasPrefix(ev.Type.String(), "EVENT_TYPE_JOB_") && ev.JobId == "" {
			return fmt.Errorf("缺少任务 ID")
		}
		jobAuthorized := false
		if ev.JobId != "" {
			j, err := jobs.Get(ctx, ev.JobId)
			if err != nil || j == nil || j.WorkstationID != ws {
				return fmt.Errorf("任务不属于当前节点")
			}
			if ev.EmployeeId != "" && ev.EmployeeId != j.EmployeeID {
				return fmt.Errorf("员工与任务不符")
			}
			ev.EmployeeId = j.EmployeeID
			jobAuthorized = true
			if p["workspace_id"] != "" && p["workspace_id"] != j.WorkspaceID {
				return fmt.Errorf("工作区与任务不符")
			}
		}
		if ev.EmployeeId != "" {
			e, err := employees.Get(ctx, ev.EmployeeId)
			if err != nil || e == nil || (!jobAuthorized && e.WorkstationID != ws) {
				return fmt.Errorf("员工不属于当前节点")
			}
		}
		if ev.SessionId != "" {
			existing, err := sessions.Get(ctx, ev.SessionId)
			if err == nil && existing != nil && (existing.WorkstationID != ws || existing.EmployeeID != ev.EmployeeId) {
				return fmt.Errorf("会话不属于当前节点或员工")
			}
			if err != nil && err != session.ErrNotFound {
				return err
			}
			if ev.EmployeeId == "" {
				return fmt.Errorf("会话缺少员工")
			}
		}
		return nil
	}
}

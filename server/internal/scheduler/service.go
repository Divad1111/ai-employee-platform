// Package scheduler V1 最小调度：选 ONLINE Workstation 并下发 START_JOB。
// Offline / DRAINING 不分新 Job。
// 设计依据：设计文档 §17、§58、§91、§93、§94。
package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/mcp"
	"github.com/ai-employee-platform/server/internal/mcpauth"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/workflowmcp"
	"github.com/ai-employee-platform/server/internal/workspace"
	"github.com/ai-employee-platform/server/internal/workstation"
)

// 错误。
var (
	ErrNoWorkstation = errors.New("无可用 Workstation（需 ONLINE）")
	ErrNotAssignable = errors.New("Employee 不可调度")
)

// CommandPusher 下发命令。
type CommandPusher interface {
	PushCommand(wsID string, typ aiev1.CommandType, employeeID, jobID, payloadJSON string) (*aiev1.Command, error)
}

// Service 调度器。
type Service struct {
	mu           sync.Mutex
	Jobs         *job.Service
	Employees    *employee.Service
	Workstations *workstation.Service
	Workspaces   *workspace.Service
	Presence     *reliability.Presence
	Pusher       CommandPusher
	FullPusher   FullCommandPusher
	// OnTerminal 调度器自行把任务打到终态时回调，用于向飞书回执失败。
	OnTerminal         func(ctx context.Context, j *job.Job)
	WorkflowMCP        *workflowmcp.Service
	MCPAuth            *mcpauth.Service
	MCP                *mcp.Service
	MCPPublicURL       string // 例如 http://127.0.0.1:8080/mcp
	MaxConcurrentPerWS int
	MaxCPUPercent      float64 // 超过则不分新 Job（0=不限制）
	MaxMemoryPercent   float64
	MaxSessionsPerWS   uint32
	runningOnWS        map[string]int
	lastRejectReason   map[string]string // wsID → 原因（Admin 可见）
}

// New 创建。
func New(jobs *job.Service, emps *employee.Service, wss *workstation.Service, presence *reliability.Presence, pusher CommandPusher) *Service {
	return &Service{
		Jobs: jobs, Employees: emps, Workstations: wss, Presence: presence, Pusher: pusher,
		MaxConcurrentPerWS: 2,
		MaxCPUPercent:      90,
		MaxMemoryPercent:   90,
		MaxSessionsPerWS:   8,
		runningOnWS:        map[string]int{},
		lastRejectReason:   map[string]string{},
	}
}

// SetWorkspaces 注入工作区查询（下发 path）。
func (s *Service) SetWorkspaces(ws *workspace.Service) { s.Workspaces = ws }

// ScheduleJob 将 CREATED/QUEUED Job 分配到可用 WS 并下发 START_JOB。
func (s *Service) ScheduleJob(ctx context.Context, jobID string) (*job.Job, error) {
	j, err := s.Jobs.Get(ctx, jobID)
	if err != nil {
		return nil, err
	}
	emp, err := s.Employees.Get(ctx, j.EmployeeID)
	if err != nil {
		return nil, ErrNotAssignable
	}
	if emp.Status == employee.StatusDisabled {
		return nil, ErrNotAssignable
	}
	wsID := emp.WorkstationID
	if j.WorkspaceID != "" && s.Workspaces != nil {
		if wsp, err := s.Workspaces.Get(ctx, j.WorkspaceID); err == nil && wsp != nil {
			if wsID == "" && wsp.WorkstationID != "" {
				wsID = wsp.WorkstationID
			}
		}
	}
	if wsID == "" {
		for _, v := range s.Workstations.List(ctx) {
			if v.Status == reliability.StatusOnline && s.canAssign(v.ID) {
				wsID = v.ID
				break
			}
		}
	}
	if wsID == "" || !s.isOnline(wsID) || !s.canAssign(wsID) {
		return nil, ErrNoWorkstation
	}
	if reason := s.resourceBlockReason(wsID); reason != "" {
		s.mu.Lock()
		s.lastRejectReason[wsID] = reason
		s.mu.Unlock()
		return nil, errors.New(reason)
	}
	if emp.WorkspaceID == "" && j.WorkspaceID == "" {
		return nil, errors.New("Workspace Missing")
	}
	if j.WorkspaceID == "" {
		j.WorkspaceID = emp.WorkspaceID
	}

	if j.Status == job.StatusCreated {
		if _, err = s.Jobs.Transition(ctx, jobID, job.StatusQueued, "scheduler", "", nil); err != nil {
			return nil, err
		}
	}
	if _, err = s.Jobs.Transition(ctx, jobID, job.StatusAssigned, "scheduler", "", map[string]string{"workstation_id": wsID}); err != nil {
		return nil, err
	}
	if err := s.Jobs.BindWorkstation(ctx, jobID, wsID); err != nil {
		return nil, err
	}
	j, _ = s.Jobs.Get(ctx, jobID)

	if s.Pusher != nil {
		payload := map[string]string{
			"prompt":       j.Prompt,
			"workspace_id": j.WorkspaceID,
		}
		if s.Employees != nil && j.EmployeeID != "" {
			if e, err := s.Employees.Get(ctx, j.EmployeeID); err == nil && e != nil && e.DefaultModel != "" {
				payload["model"] = e.DefaultModel
			}
		}
		workspacePath := ""
		if s.Workspaces != nil && j.WorkspaceID != "" {
			if wsp, err := s.Workspaces.Get(ctx, j.WorkspaceID); err == nil && wsp != nil && wsp.Path != "" {
				payload["workspace_path"] = wsp.Path
				workspacePath = wsp.Path
			}
		}
		payloadJSON, _ := json.Marshal(payload)

		var startJob *aiev1.StartJobPayload
		if s.WorkflowMCP != nil {
			var err error
			startJob, err = s.buildStartJobPayload(ctx, j.EmployeeID, j.Prompt, j.WorkspaceID, workspacePath, j.WorkflowID)
			if err != nil {
				return j, err
			}
			if startJob != nil && startJob.WorkflowId != "" {
				j.WorkflowID = startJob.WorkflowId
				j.WorkflowSnapshot = map[string]any{
					"id": startJob.WorkflowId, "version": startJob.WorkflowVersion,
				}
				if s.Jobs != nil {
					_ = s.Jobs.SetWorkflowSnapshot(ctx, j.ID, j.WorkflowID, j.WorkflowSnapshot)
				}
			}
		}

		if s.FullPusher != nil && startJob != nil {
			if _, err := s.FullPusher.PushCommandFull(wsID, aiev1.CommandType_COMMAND_TYPE_START_JOB, j.EmployeeID, j.ID, string(payloadJSON), func(cmd *aiev1.Command) {
				cmd.Structured = &aiev1.Command_StartJob{StartJob: startJob}
			}); err != nil {
				return j, err
			}
		} else if _, err := s.Pusher.PushCommand(wsID, aiev1.CommandType_COMMAND_TYPE_START_JOB, j.EmployeeID, j.ID, string(payloadJSON)); err != nil {
			return j, err
		}
	}
	s.mu.Lock()
	s.runningOnWS[wsID]++
	s.mu.Unlock()

	return s.Jobs.Transition(ctx, jobID, job.StatusStarting, "scheduler", "", nil)
}

func (s *Service) isOnline(wsID string) bool {
	if s.Presence != nil && !s.Presence.IsSchedulable(wsID) {
		return false
	}
	v := s.Workstations.Get(context.Background(), wsID)
	return v.Status == reliability.StatusOnline
}

func (s *Service) canAssign(wsID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runningOnWS[wsID] < s.MaxConcurrentPerWS
}

// resourceBlockReason 按 CPU/Memory/Session 限额；超限返回可读原因。
func (s *Service) resourceBlockReason(wsID string) string {
	if s.Presence == nil {
		return ""
	}
	st, _, cpu, mem, _, sessions, ok := s.Presence.ResourceSnapshot(wsID)
	if !ok || st != reliability.StatusOnline {
		return ""
	}
	if s.MaxCPUPercent > 0 && cpu >= s.MaxCPUPercent {
		return "资源不足: CPU 超限，Job 保持排队"
	}
	if s.MaxMemoryPercent > 0 && mem >= s.MaxMemoryPercent {
		return "资源不足: Memory 超限，Job 保持排队"
	}
	if s.MaxSessionsPerWS > 0 && sessions >= s.MaxSessionsPerWS {
		return "资源不足: Session 并发超限，Job 保持排队"
	}
	return ""
}

// RejectReasons Admin 可见的最近拒绝原因。
func (s *Service) RejectReasons() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for k, v := range s.lastRejectReason {
		out[k] = v
	}
	return out
}

// Release 任务终态后释放配额。
func (s *Service) Release(wsID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runningOnWS[wsID] > 0 {
		s.runningOnWS[wsID]--
	}
}

// Tick 扫描 CREATED/QUEUED Job 尝试调度，并扫描超时卡死在 STARTING 的 Job。
func (s *Service) Tick(ctx context.Context) (scheduled int) {
	list, err := s.Jobs.List(ctx)
	if err != nil {
		return 0
	}
	now := time.Now().UTC()
	for _, j := range list {
		// 检查长时间卡在 STARTING 的 Job（> 60秒）：超时失败并释放工作站并发槽位
		if j.Status == job.StatusStarting {
			startedAt := j.StartedAt
			if startedAt.IsZero() {
				startedAt = j.CreatedAt
			}
			// 命令还没送到工作站时先不判失败，等重连续传。
			if s.startStillPending(j.WorkstationID, j.ID) && now.Sub(startedAt) <= 10*time.Minute {
				continue
			}
			if now.Sub(startedAt) > 60*time.Second {
				uj, terr := s.Jobs.Transition(ctx, j.ID, job.StatusFailed, "scheduler", "", map[string]string{
					"reason": "工作站节点启动超时 (60s 无响应)",
					"error":  "工作站节点启动超时 (60s 无响应)",
				})
				if j.WorkstationID != "" {
					s.Release(j.WorkstationID)
				}
				if terr == nil && uj != nil && s.OnTerminal != nil {
					s.OnTerminal(ctx, uj)
				}
			}
		}

		if j.Status == job.StatusCreated || j.Status == job.StatusQueued {
			if _, err := s.ScheduleJob(ctx, j.ID); err == nil {
				scheduled++
			} else {
				fmt.Printf("[Scheduler] ScheduleJob(%s) 失败: %v (wsID=%s online=%v)\n", j.ID, err, j.WorkstationID, func() bool {
					if j.WorkstationID != "" {
						return s.isOnline(j.WorkstationID)
					}
					return false
				}())
			}
		}
	}
	return scheduled
}

func (s *Service) startStillPending(wsID, jobID string) bool {
	type pending interface {
		StartCommandUnacked(wsID, jobID string) bool
	}
	if p, ok := s.Pusher.(pending); ok {
		return p.StartCommandUnacked(wsID, jobID)
	}
	if p, ok := s.FullPusher.(pending); ok {
		return p.StartCommandUnacked(wsID, jobID)
	}
	return false
}

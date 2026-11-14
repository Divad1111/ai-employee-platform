// Package scheduler V1 最小调度：选 ONLINE Workstation 并下发 START_JOB。
// Offline / DRAINING 不分新 Job。
// 设计依据：设计文档 §17、§58、§91、§93、§94。
package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ai-employee-platform/gen/workspacedir"
	"slices"
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
	ErrNoWorkstation       = errors.New("无可用 Workstation（需 ONLINE）")
	ErrNotAssignable       = errors.New("Employee 不可调度")
	ErrEmployeeBusy        = errors.New("Employee 已有任务正在执行")
	ErrConversationQueued  = errors.New("员工会话中有更早的任务等待执行")
	ErrResourceUnavailable = errors.New("工作站资源暂时不足")
)

type taskQueueGate interface {
	CanScheduleJob(context.Context, string) (bool, error)
}

type taskQueueContext interface {
	ConversationContext(context.Context, string) (string, error)
}

type sessionMCPToken struct {
	secret    string
	expiresAt time.Time
}

// CommandPusher 下发命令。
type CommandPusher interface {
	PushCommand(wsID string, typ aiev1.CommandType, employeeID, jobID, payloadJSON string) (*aiev1.Command, error)
}

// Service 调度器。
type Service struct {
	mu           sync.Mutex
	scheduleMu   sync.Mutex
	mcpTokenMu   sync.Mutex
	mcpTokens    map[string]sessionMCPToken
	Jobs         *job.Service
	Employees    *employee.Service
	Workstations *workstation.Service
	Workspaces   *workspace.Service
	Presence     *reliability.Presence
	Pusher       CommandPusher
	FullPusher   FullCommandPusher
	TaskQueue    taskQueueGate
	// Maintenance 在基础设施维护期间暂停分配新任务，已有任务可以正常结束。
	Maintenance func(context.Context, string) (bool, error)
	// AuthorizeAssignment 在实际派发时复核当前用户和节点授权。
	AuthorizeAssignment func(context.Context, *job.Job, *employee.Employee, string) error
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
		mcpTokens:          map[string]sessionMCPToken{},
	}
}

// SetWorkspaces 注入工作区查询（下发 path）。
func (s *Service) SetWorkspaces(ws *workspace.Service) { s.Workspaces = ws }

func (s *Service) SetTaskQueue(gate taskQueueGate) { s.TaskQueue = gate }

// ScheduleJob 将 CREATED/QUEUED Job 分配到可用 WS 并下发 START_JOB。
func (s *Service) ScheduleJob(ctx context.Context, jobID string) (scheduled *job.Job, scheduleErr error) {
	s.scheduleMu.Lock()
	var failedJob *job.Job
	var scheduling bool
	var reservedWS string
	var startCommand *aiev1.Command
	defer func() {
		s.scheduleMu.Unlock()
		if failedJob != nil && s.OnTerminal != nil {
			s.OnTerminal(context.WithoutCancel(ctx), failedJob)
		}
	}()
	defer func() {
		if scheduleErr == nil || !scheduling {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if isSchedulingWait(scheduleErr) {
			if err := s.Jobs.ReportSchedulingBlock(cleanupCtx, jobID, scheduleErr.Error()); err != nil {
				scheduleErr = errors.Join(scheduleErr, err)
			}
			return
		}
		var err error
		failedJob, err = s.Jobs.Transition(cleanupCtx, jobID, job.StatusFailed, "scheduler", "", map[string]string{
			"reason": "调度失败", "error": scheduleErr.Error(), "phase": "scheduling",
		})
		if err != nil {
			scheduleErr = errors.Join(scheduleErr, fmt.Errorf("保存调度失败状态失败: %w", err))
		} else {
			scheduled = failedJob
		}
		if reservedWS != "" {
			s.Release(reservedWS)
		}
		if startCommand != nil {
			s.abortStartCommand(startCommand, scheduleErr.Error())
		}
	}()
	j, err := s.Jobs.Get(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if j.Status != job.StatusCreated && j.Status != job.StatusQueued {
		return nil, fmt.Errorf("%w: 任务状态 %s 不可调度", job.ErrInvalidTransition, j.Status)
	}
	scheduling = true
	if s.TaskQueue != nil {
		allowed, gateErr := s.TaskQueue.CanScheduleJob(ctx, jobID)
		if gateErr != nil {
			return nil, gateErr
		}
		if !allowed {
			return nil, ErrConversationQueued
		}
	}
	jobs, listErr := s.Jobs.List(ctx)
	if listErr != nil {
		return nil, listErr
	}
	for _, other := range jobs {
		if other.ID == jobID || other.EmployeeID != j.EmployeeID {
			continue
		}
		switch other.Status {
		case job.StatusAssigned, job.StatusStarting, job.StatusRunning, job.StatusBlocked, job.StatusWaitingApproval, job.StatusUnknown:
			return nil, ErrEmployeeBusy
		}
	}
	emp, err := s.Employees.Get(ctx, j.EmployeeID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotAssignable, err)
	}
	if emp.Status == employee.StatusDisabled {
		return nil, ErrNotAssignable
	}
	if j.WorkspaceID == "" {
		j.WorkspaceID = emp.WorkspaceID
	}
	wsID := emp.WorkstationID
	if j.WorkspaceID != "" && s.Workspaces != nil {
		wsp, err := s.Workspaces.Get(ctx, j.WorkspaceID)
		if err != nil {
			return nil, fmt.Errorf("读取工作区失败: %w", err)
		}
		if wsID == "" && wsp.WorkstationID != "" {
			wsID = wsp.WorkstationID
		}
	}
	if wsID == "" {
		for _, v := range s.Workstations.List(ctx) {
			available, err := s.workstationAvailable(ctx, v.ID)
			if err != nil {
				return nil, fmt.Errorf("检查工作站调度状态失败: %w", err)
			}
			if v.Status == reliability.StatusOnline && available && s.canAssign(v.ID) {
				wsID = v.ID
				break
			}
		}
	}
	if wsID == "" {
		return nil, ErrNoWorkstation
	}
	available, err := s.workstationAvailable(ctx, wsID)
	if err != nil {
		return nil, fmt.Errorf("检查工作站调度状态失败: %w", err)
	}
	if !available || !s.canAssign(wsID) {
		return nil, ErrNoWorkstation
	}
	if reason := s.resourceBlockReason(wsID); reason != "" {
		s.mu.Lock()
		s.lastRejectReason[wsID] = reason
		s.mu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrResourceUnavailable, reason)
	}
	if emp.WorkspaceID == "" && j.WorkspaceID == "" {
		return nil, errors.New("Workspace Missing")
	}

	workspaceID := j.WorkspaceID
	var workspacePath, rootPath, userDir string
	if s.Workspaces != nil && workspaceID != "" {
		wsp, err := s.Workspaces.Get(ctx, workspaceID)
		if err != nil || wsp.WorkstationID != wsID || (wsp.EmployeeID != "" && wsp.EmployeeID != emp.ID) {
			return nil, errors.New("工作区、员工与工作站不一致")
		}
		workspacePath = wsp.Path
		if wsp.OwnerUserID != "" {
			if wsp.OwnerUserID != emp.OwnerUserID || !workspacedir.ValidName(wsp.Path) || !workspacedir.ValidName(wsp.UserDirectory) {
				return nil, errors.New("工作区用户或目录不匹配")
			}
			roots, err := s.Workstations.Meta.WorkspaceRoots(ctx, wsID)
			if err != nil || !slices.Contains(roots, wsp.RootPath) {
				return nil, errors.New("工作站已移除该工作区的根目录，请重新绑定")
			}
			rootPath, userDir = wsp.RootPath, wsp.UserDirectory
		}
	}
	if s.AuthorizeAssignment != nil {
		if err := s.AuthorizeAssignment(ctx, j, emp, wsID); err != nil {
			return nil, err
		}
	}
	// 在占用员工前完成凭证和上下文检查；异常由统一出口直接置为失败。
	var startJob *aiev1.StartJobPayload
	if s.Pusher != nil && (s.WorkflowMCP != nil || s.MCP != nil) {
		startJob, err = s.buildStartJobPayload(ctx, j.EmployeeID, j.Prompt, j.WorkspaceID, workspacePath, j.WorkflowID)
		if err != nil {
			return j, err
		}
	}
	var conversationContext string
	if s.Pusher != nil {
		if ctxProvider, ok := s.TaskQueue.(taskQueueContext); ok {
			conversationContext, err = ctxProvider.ConversationContext(ctx, j.ID)
			if err != nil {
				return j, fmt.Errorf("读取员工会话上下文失败: %w", err)
			}
		}
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
	j, err = s.Jobs.Get(ctx, jobID)
	if err != nil {
		return nil, err
	}

	if s.Pusher != nil {
		payload := map[string]any{
			"prompt":                j.Prompt,
			"workspace_id":          workspaceID,
			"provider":              emp.DefaultProvider,
			"session_generation":    emp.SessionGeneration,
			"reset_session_context": emp.SessionContextResetGeneration == emp.SessionGeneration,
			"input_attachments":     j.InputAttachments,
		}
		if conversationContext != "" {
			payload["conversation_context"] = conversationContext
		}
		if emp.DefaultModel != "" {
			payload["model"] = emp.DefaultModel
		}
		if workspacePath != "" {
			payload["workspace_path"] = workspacePath
		}
		if rootPath != "" {
			payload["workspace_root"] = rootPath
			payload["workspace_user_directory"] = userDir
		}
		payloadJSON, _ := json.Marshal(payload)

		if startJob != nil && startJob.WorkflowId != "" {
			j.WorkflowID = startJob.WorkflowId
			j.WorkflowSnapshot = map[string]any{
				"id": startJob.WorkflowId, "version": startJob.WorkflowVersion,
			}
			if s.Jobs != nil {
				if err := s.Jobs.SetWorkflowSnapshot(ctx, j.ID, j.WorkflowID, j.WorkflowSnapshot); err != nil {
					return j, err
				}
			}
		}

		j, err = s.Jobs.Transition(ctx, jobID, job.StatusStarting, "scheduler", "", nil)
		if err != nil {
			return j, err
		}
		s.mu.Lock()
		s.runningOnWS[wsID]++
		s.mu.Unlock()
		reservedWS = wsID

		if s.FullPusher != nil && startJob != nil {
			startCommand, err = s.FullPusher.PushCommandFull(wsID, aiev1.CommandType_COMMAND_TYPE_START_JOB, j.EmployeeID, j.ID, string(payloadJSON), func(cmd *aiev1.Command) {
				cmd.Structured = &aiev1.Command_StartJob{StartJob: startJob}
			})
		} else {
			startCommand, err = s.Pusher.PushCommand(wsID, aiev1.CommandType_COMMAND_TYPE_START_JOB, j.EmployeeID, j.ID, string(payloadJSON))
		}
		if err != nil {
			return j, err
		}
		if emp.SessionContextResetGeneration == emp.SessionGeneration {
			s.Employees.ConsumeSessionContextReset(ctx, emp.ID, emp.SessionGeneration)
		}
		return j, nil
	}
	s.mu.Lock()
	s.runningOnWS[wsID]++
	s.mu.Unlock()
	reservedWS = wsID

	return s.Jobs.Transition(ctx, jobID, job.StatusStarting, "scheduler", "", nil)
}

func (s *Service) isOnline(wsID string) bool {
	available, _ := s.workstationAvailable(context.Background(), wsID)
	return available
}

func (s *Service) workstationAvailable(ctx context.Context, wsID string) (bool, error) {
	if s.Maintenance != nil {
		blocked, err := s.Maintenance(ctx, wsID)
		if err != nil {
			return false, err
		}
		if blocked {
			return false, nil
		}
	}
	if s.Presence != nil && !s.Presence.IsSchedulable(wsID) {
		return false, nil
	}
	v := s.Workstations.Get(ctx, wsID)
	return v.Status == reliability.StatusOnline, nil
}

// WorkstationBusy 等待已开始的分配完成后检查运行任务，避免维护与任务派发交叉。
func (s *Service) WorkstationBusy(ctx context.Context, wsID string) (bool, error) {
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	jobs, err := s.Jobs.List(ctx)
	if err != nil {
		return true, err
	}
	for _, j := range jobs {
		if j.WorkstationID == wsID {
			switch j.Status {
			case job.StatusAssigned, job.StatusStarting, job.StatusRunning, job.StatusUnknown, job.StatusBlocked, job.StatusWaitingApproval:
				return true, nil
			}
		}
	}
	return false, nil
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

// CancelJob 取消 Job，并在已分配工作站时下发 STOP_JOB。
func (s *Service) CancelJob(ctx context.Context, jobID, actorID, ip string) (*job.Job, error) {
	j, err := s.Jobs.Cancel(ctx, jobID, actorID, ip)
	if err != nil {
		return nil, err
	}
	if j.WorkstationID != "" {
		payload, _ := json.Marshal(map[string]string{"reason": "user_cancel"})
		if s.Pusher != nil {
			if _, pushErr := s.Pusher.PushCommand(j.WorkstationID, aiev1.CommandType_COMMAND_TYPE_STOP_JOB,
				j.EmployeeID, j.ID, string(payload)); pushErr != nil {
				fmt.Printf("[Scheduler] STOP_JOB 入队失败 (job=%s, ws=%s): %v\n", j.ID, j.WorkstationID, pushErr)
			}
		}
		s.Release(j.WorkstationID)
	}
	return j, nil
}

// Tick 调度排队任务，并回收分配或启动阶段超时的任务。
func (s *Service) Tick(ctx context.Context) (scheduled int) {
	list, err := s.Jobs.List(ctx)
	if err != nil {
		return 0
	}
	now := time.Now().UTC()
	for _, j := range list {
		if j.Status == job.StatusAssigned {
			s.failAbandonedAssignment(ctx, j.ID)
		}
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
			} else if !errors.Is(err, ErrConversationQueued) && !errors.Is(err, ErrEmployeeBusy) {
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

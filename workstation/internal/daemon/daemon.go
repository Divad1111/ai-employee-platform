// Package daemon 实现 aew daemon 启动流水线。
// Load Config → Identity → Store/Recover → IPC → Connect → Heartbeat → Ready。
// 设计依据：设计文档 §123、§68、§89。
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/workstation/internal/acp"
	"github.com/ai-employee-platform/workstation/internal/artifactlocal"
	"github.com/ai-employee-platform/workstation/internal/config"
	"github.com/ai-employee-platform/workstation/internal/controlplane/ack"
	grpcclient "github.com/ai-employee-platform/workstation/internal/controlplane/grpc"
	"github.com/ai-employee-platform/workstation/internal/controlplane/heartbeat"
	"github.com/ai-employee-platform/workstation/internal/controlplane/outbox"
	"github.com/ai-employee-platform/workstation/internal/controlplane/reconnect"
	"github.com/ai-employee-platform/workstation/internal/identity"
	"github.com/ai-employee-platform/workstation/internal/ipc"
	"github.com/ai-employee-platform/workstation/internal/monitor"
	"github.com/ai-employee-platform/workstation/internal/platform"
	"github.com/ai-employee-platform/workstation/internal/providers"
	"github.com/ai-employee-platform/workstation/internal/providers/antigravity"
	"github.com/ai-employee-platform/workstation/internal/providers/codex"
	"github.com/ai-employee-platform/workstation/internal/providers/cursor"
	"github.com/ai-employee-platform/workstation/internal/runtime"
	"github.com/ai-employee-platform/workstation/internal/runtime/process"
	"github.com/ai-employee-platform/workstation/internal/runtime/recovery"
	"github.com/ai-employee-platform/workstation/internal/service"
	"github.com/ai-employee-platform/workstation/internal/skillsync"
)

// Options 启动选项。
type Options struct {
	Paths     platform.Paths
	Config    *config.Config
	Transport ipc.Transport
	// SkipConnect 测试时可跳过 gRPC
	SkipConnect bool
	Journal     ack.Journal
}

// Daemon 运行时实例。
type Daemon struct {
	Opts      Options
	Runtime   *runtime.Managers
	Recovery  *recovery.Manager
	Proc      *process.Manager
	Registry  *providers.Registry
	Monitor   *monitor.Sampler
	Outbox    *outbox.MemoryStore
	Backoff   *reconnect.Backoff
	Ready     bool
	Sample    monitor.Sample
	eventSeq  atomic.Uint64 // Control Plane 事件严格递增序号（不可为 0）
	wsID      string
	Artifacts *artifactlocal.Queue
	ArtUpload *artifactlocal.Uploader

	modelMu         sync.Mutex
	modelCache      []heartbeat.ModelInfo
	modelRefreshing bool
	modelAgain      bool

	inqMu            sync.Mutex
	pendingInquiries map[string]chan string
	activeSessMu     sync.Mutex
	activeSess       *grpcclient.Session
	cancelMu         sync.Mutex
	cancel           context.CancelFunc
}

// New 组装 Daemon。
func New(opts Options) *Daemon {
	if opts.Config == nil {
		opts.Config = config.Default()
	}
	if opts.Paths == nil {
		opts.Paths = platform.Detect()
	}
	if opts.Transport == nil {
		opts.Transport = ipc.DefaultTransport()
	}
	if opts.Journal == nil {
		opts.Journal = ack.NewMemoryJournal()
	}
	proc := process.NewManager(
		"agent", "agent.exe", "agent.cmd", "agent-fake", "cursor-fake",
		"codex", "codex.exe", "codex-fake",
		"antigravity", "antigravity.exe", "antigravity.cmd", "agy", "agy.exe", "agy.cmd",
		"language_server", "language_server.exe", "agentapi.bat", "antigravity-fake",
	)
	reg := providers.NewRegistry()
	curPath, codPath, agyPath := "", "", ""
	if opts.Config.Providers != nil {
		if p, ok := opts.Config.Providers["cursor"]; ok {
			curPath = p.Path
		}
		if p, ok := opts.Config.Providers["codex"]; ok {
			codPath = p.Path
		}
		if p, ok := opts.Config.Providers["antigravity"]; ok {
			agyPath = p.Path
		}
	}
	reg.Register(cursor.NewProvider(proc, curPath))
	reg.Register(codex.NewProvider(proc, codPath))
	reg.Register(antigravity.NewProvider(proc, agyPath))
	rt := runtime.NewManagers(opts.Paths, reg)
	rt.MaxSess = opts.Config.MaxSessions
	ob := outbox.NewMemoryStore()
	// 本地 runtime 状态不走 CP Outbox：否则会污染事件序号，导致 JOB_* 被 EventStore 拒绝。
	rt.Sink = nil
	d := &Daemon{
		Opts:     opts,
		Runtime:  rt,
		Proc:     proc,
		Registry: reg,
		Recovery: &recovery.Manager{Runtime: rt, Proc: proc},
		Monitor:  &monitor.Sampler{},
		Outbox:   ob,
		Backoff:  reconnect.NewBackoff(60 * time.Second),
	}
	artRoot := opts.Paths.DataDir()
	if artRoot == "" {
		artRoot = opts.Paths.IdentityDir()
	}
	d.Artifacts = artifactlocal.New(artRoot)
	if b, err := identity.Load(opts.Paths); err == nil && b != nil {
		d.wsID = b.WorkstationID
		d.ArtUpload = &artifactlocal.Uploader{
			HTTPBase: opts.Config.ControlPlaneHTTPEndpoint,
			Bundle:   b,
			Queue:    d.Artifacts,
		}
	}
	d.pendingInquiries = make(map[string]chan string)
	d.Runtime.SetInquiryCallback(func(ctx context.Context, jobID, empID string, inq acp.Inquiry) (string, error) {
		return d.handleAgentInquiry(ctx, jobID, empID, inq)
	})
	return d
}

// newEvent 构造带严格递增 Sequence 的事件（CP EventStore 要求 sequence > 0）。
func (d *Daemon) newEvent(jobID, sessionID, employeeID string, typ aiev1.EventType, payload string) *aiev1.Event {
	seq := d.eventSeq.Add(1)
	now := time.Now()
	idKey := jobID
	if idKey == "" {
		idKey = sessionID
	}
	if idKey == "" {
		idKey = "sys"
	}
	return &aiev1.Event{
		EventId:       fmt.Sprintf("ev-%s-%d", idKey, seq),
		WorkstationId: d.wsID,
		JobId:         jobID,
		SessionId:     sessionID,
		EmployeeId:    employeeID,
		Type:          typ,
		Meta: &aiev1.EnvelopeMeta{
			MessageId:       fmt.Sprintf("msg-%s-%d", idKey, seq),
			Sequence:        seq,
			TimestampUnixMs: now.UnixMilli(),
		},
		PayloadJson: payload,
	}
}

func (d *Daemon) newJobEvent(jobID string, typ aiev1.EventType, payload string) *aiev1.Event {
	return d.newEvent(jobID, "", "", typ, payload)
}

// Run 阻塞运行直到 ctx 取消。
func (d *Daemon) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.cancelMu.Lock()
	d.cancel = cancel
	d.cancelMu.Unlock()

	paths := d.Opts.Paths
	_ = os.MkdirAll(paths.DataDir(), 0o755)
	_ = os.MkdirAll(paths.LogDir(), 0o755)

	// Identity
	if _, err := identity.Load(paths); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 未注册身份（%v），Control Plane 连接将跳过\n", err)
		d.Opts.SkipConnect = true
	}

	// Recover
	d.Recovery.BootRecover()

	// IPC
	ipcSrv := &ipc.Server{
		Transport: d.Opts.Transport,
		Handler:   d.handleIPC,
	}
	go func() { _ = ipcSrv.Serve(ctx) }()

	// Connect + Heartbeat（可选）
	if !d.Opts.SkipConnect {
		go d.maintainControlPlane(ctx)
	}

	d.Ready = true
	fmt.Printf("aew daemon ready\nIPC=%s\n", d.Opts.Transport.Addr())

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			d.Ready = false
			return nil
		case <-ticker.C:
			d.Sample = d.Monitor.Sample()
		}
	}
}

func (d *Daemon) maintainControlPlane(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		b, err := identity.Load(d.Opts.Paths)
		if err != nil {
			time.Sleep(d.Backoff.BeginReconnect())
			continue
		}
		cli, err := grpcclient.Dial(ctx, d.Opts.Config.ControlPlaneEndpoint, b)
		if err != nil {
			d.Backoff.MarkDisconnected()
			time.Sleep(d.Backoff.BeginReconnect())
			continue
		}
		sess := grpcclient.NewSession(cli, d.Opts.Journal)
		sess.Outbox = &outbox.Dispatcher{Store: d.Outbox}
		sess.HeartbeatInterval = 5 * time.Second
		// 连上控制面（注册/重连）后上报一次模型，不放进周期心跳。
		sess.OnConnected = func() {
			go d.reportModels(context.Background())
		}
		// 连接成功后刷新 ArtUpload 身份，并冲刷待上传队列
		d.ArtUpload = &artifactlocal.Uploader{
			HTTPBase: d.Opts.Config.ControlPlaneHTTPEndpoint,
			Bundle:   b,
			Queue:    d.Artifacts,
		}
		go func() {
			_, _ = d.ArtUpload.Flush()
		}()
		sess.Stats = func() heartbeat.Stats {
			e, _, s, _ := d.Runtime.Snapshot()
			sample := d.Sample
			if sample.CPUPercent == 0 && sample.MemoryPercent == 0 && d.Monitor != nil {
				sample = d.Monitor.Sample()
			}
			return heartbeat.Stats{
				Employees: uint32(e),
				Sessions:  uint32(s),
				CPU:       sample.CPUPercent,
				Memory:    sample.MemoryPercent,
				Disk:      sample.DiskPercent,
				Providers: d.installedProviders(context.Background()),
			}
		}
		sess.OnCommand = func(cctx context.Context, cmd *aiev1.Command) error {
			return d.handleCommand(cctx, sess, cmd)
		}
		d.Backoff.MarkConnected()
		d.setActiveSession(sess)
		_ = sess.Run(ctx)
		d.setActiveSession(nil)
		_ = cli.Close()
		d.Backoff.MarkDisconnected()
		if ctx.Err() != nil {
			return
		}
		time.Sleep(d.Backoff.BeginReconnect())
	}
}

func (d *Daemon) handleCommand(ctx context.Context, sess *grpcclient.Session, cmd *aiev1.Command) error {
	switch cmd.GetType() {
	case aiev1.CommandType_COMMAND_TYPE_SYNC_SKILLS:
		payload := cmd.GetSyncSkills()
		if payload == nil {
			return nil
		}
		_, err := skillsync.Sync(payload.GetPackages(), payload.GetTargetDir(), payload.GetPruneCursorNames())
		return err

	case aiev1.CommandType_COMMAND_TYPE_START_JOB:
		jobID := cmd.GetJobId()
		empID := cmd.GetEmployeeId()
		if jobID == "" || empID == "" {
			return nil
		}
		var payload struct {
			Prompt        string `json:"prompt"`
			WorkspaceID   string `json:"workspace_id"`
			WorkspacePath string `json:"workspace_path"`
			Model         string `json:"model"`
		}
		if pJSON := cmd.GetPayloadJson(); pJSON != "" {
			_ = json.Unmarshal([]byte(pJSON), &payload)
		}
		startJob := cmd.GetStartJob()
		if startJob != nil {
			if startJob.GetPrompt() != "" {
				payload.Prompt = startJob.GetPrompt()
			}
			if startJob.GetWorkspaceId() != "" {
				payload.WorkspaceID = startJob.GetWorkspaceId()
			}
			if startJob.GetWorkspacePath() != "" {
				payload.WorkspacePath = startJob.GetWorkspacePath()
			}
			// 先落盘技能包，再 StartSession
			_, _ = skillsync.SyncFromStartJob(startJob)
		}

		// 确保本地员工视图，驱动引擎来自中心下发的员工配置
		provider := "cursor"
		if startJob != nil && startJob.GetProvider() != "" {
			provider = startJob.GetProvider()
		}
		_, _ = d.Runtime.EnsureEmployee(empID, empID, provider)

		// 确保工作区视图（使用 Control Plane 下发的本机路径，禁止静默落到默认目录）
		wsID := payload.WorkspaceID
		if wsID != "" {
			_, _ = d.Runtime.EnsureWorkspace(wsID, empID, payload.WorkspacePath)
		}

		// 将会话 MCP 配置转为 []any
		var mcpServers []any
		if startJob != nil {
			for _, m := range startJob.GetMcpServers() {
				entry := map[string]any{"name": m.GetName()}
				if m.GetType() != "" {
					entry["type"] = m.GetType()
				}
				if m.GetCommand() != "" {
					entry["command"] = m.GetCommand()
				}
				if len(m.GetArgs()) > 0 {
					entry["args"] = m.GetArgs()
				}
				if len(m.GetEnv()) > 0 {
					entry["env"] = m.GetEnv()
				}
				if m.GetUrl() != "" {
					entry["url"] = m.GetUrl()
				}
				if len(m.GetHeaders()) > 0 {
					entry["headers"] = m.GetHeaders()
				}
				mcpServers = append(mcpServers, entry)
			}
		}

		// 会话复用或按需创建；MCP 变化时需重建
		var sessID string
		activeSess := d.Runtime.FindActiveSession(empID)
		if activeSess != nil && len(mcpServers) == 0 && activeSess.Provider == provider && activeSess.Model == payload.Model {
			sessID = activeSess.ID
		} else {
			if activeSess != nil {
				_ = d.Runtime.StopSession(ctx, activeSess.ID)
			}
			sessID = "ses-" + empID
			_ = sess.EnqueueEvent(d.newEvent(jobID, sessID, empID, aiev1.EventType_EVENT_TYPE_SESSION_STARTED,
				fmt.Sprintf(`{"status":"STARTING","provider":%q,"workspace_id":%q}`, provider, wsID)))
			startCtx, startCancel := context.WithTimeout(ctx, 2*time.Minute)
			_, err := d.Runtime.StartSessionWithMCP(startCtx, sessID, empID, wsID, provider, mcpServers, payload.Model)
			startCancel()
			if err != nil && err != runtime.ErrActiveSession {
				_ = sess.EnqueueEvent(d.newEvent(jobID, sessID, empID, aiev1.EventType_EVENT_TYPE_SESSION_ERROR,
					fmt.Sprintf(`{"status":"ERROR","error":%q}`, err.Error())))
				_ = sess.EnqueueEvent(d.newEvent(jobID, sessID, empID, aiev1.EventType_EVENT_TYPE_JOB_FAILED,
					fmt.Sprintf(`{"error":%q,"session_id":%q}`, err.Error(), sessID)))
				return err
			}
			_ = sess.EnqueueEvent(d.newEvent(jobID, sessID, empID, aiev1.EventType_EVENT_TYPE_SESSION_READY,
				fmt.Sprintf(`{"status":"READY","provider":%q,"workspace_id":%q}`, provider, wsID)))
		}

		_ = sess.EnqueueEvent(d.newEvent(jobID, sessID, empID, aiev1.EventType_EVENT_TYPE_JOB_STARTED,
			fmt.Sprintf(`{"status":"running","session_id":%q,"provider":%q}`, sessID, provider)))

		go func() {
			jobCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			localJob, reply, rerr := d.Runtime.RunJob(jobCtx, jobID, empID, sessID, promptWithMarkdownDefault(payload.Prompt))
			usage := map[string]string{
				"session_id":    sessID,
				"reply":         reply,
				"input_tokens":  "0",
				"output_tokens": "0",
				"total_tokens":  "0",
				"agent":         "",
				"token_source":  "unavailable",
				"usage_status":  "UNAVAILABLE",
			}
			if localJob != nil {
				usage["input_tokens"] = strconv.FormatInt(localJob.InputTokens, 10)
				usage["output_tokens"] = strconv.FormatInt(localJob.OutputTokens, 10)
				usage["total_tokens"] = strconv.FormatInt(localJob.TotalTokens, 10)
				usage["agent"] = localJob.Agent
				usage["token_source"] = localJob.TokenSource
				usage["usage_status"] = localJob.UsageStatus
				if localJob.TokenUsage != nil {
					u := localJob.TokenUsage
					usage["cached_input_tokens"] = strconv.FormatInt(u.CachedInputTokens, 10)
					usage["cache_write_input_tokens"] = strconv.FormatInt(u.CacheWriteInputTokens, 10)
					usage["cache_read_input_tokens"] = strconv.FormatInt(u.CacheReadInputTokens, 10)
					usage["reasoning_output_tokens"] = strconv.FormatInt(u.ReasoningOutputTokens, 10)
					usage["provider"] = u.Provider
					usage["provider_session_id"] = u.ProviderSessionID
					usage["provider_run_id"] = u.ProviderRunID
					usage["usage_source"] = u.Source
					if u.TotalTokens > 0 {
						usage["total_tokens"] = strconv.FormatInt(u.TotalTokens, 10)
					}
					if u.UsageStatus != "" {
						usage["usage_status"] = u.UsageStatus
					}
				}
			}
			if rerr != nil {
				msg := rerr.Error()
				usage["error"] = msg
				pl, _ := json.Marshal(usage)
				_ = sess.EnqueueEvent(d.newEvent(jobID, sessID, empID, aiev1.EventType_EVENT_TYPE_JOB_FAILED, string(pl)))
				if reply != "" {
					d.stageJobArtifact(jobID, "result-partial.md", "md", []byte(reply))
				}
				return
			}
			usage["status"] = "success"
			usage["message"] = "Job executed successfully"
			pl, _ := json.Marshal(usage)
			_ = sess.EnqueueEvent(d.newEvent(jobID, sessID, empID, aiev1.EventType_EVENT_TYPE_JOB_SUCCESS, string(pl)))
			// 文本制品默认 Markdown；用户在任务里明确要求其他格式时，正文仍按其要求。
			if reply == "" {
				reply = "任务已完成，但没有文本回复。\n"
			}
			d.stageJobArtifact(jobID, "result.md", "md", []byte(reply))
			// Job 结束后会话回到 READY（可复用）
			_ = sess.EnqueueEvent(d.newEvent(jobID, sessID, empID, aiev1.EventType_EVENT_TYPE_SESSION_READY,
				`{"status":"READY"}`))
		}()
		return nil

	case aiev1.CommandType_COMMAND_TYPE_STOP_JOB:
		d.Runtime.MarkUnknown("", cmd.GetJobId())
		return nil

	case aiev1.CommandType_COMMAND_TYPE_RESOLVE_INQUIRY:
		var p struct {
			InquiryID        string `json:"inquiry_id"`
			SelectedOptionID string `json:"selected_option_id"`
		}
		if pJSON := cmd.GetPayloadJson(); pJSON != "" {
			_ = json.Unmarshal([]byte(pJSON), &p)
		}
		d.resolveInquiry(p.InquiryID, p.SelectedOptionID)
		return nil

	case aiev1.CommandType_COMMAND_TYPE_UPDATE_WORKSTATION:
		var p struct {
			Op string `json:"op"`
		}
		if pJSON := cmd.GetPayloadJson(); pJSON != "" {
			_ = json.Unmarshal([]byte(pJSON), &p)
		}
		if p.Op == "refresh_models" {
			go d.reportModels(context.Background())
		}
		return nil

	case aiev1.CommandType_COMMAND_TYPE_SHUTDOWN:
		// 服务端通知本工作站已被删除：通知 aew 自动停止运行并清理本地证书
		go func() {
			time.Sleep(150 * time.Millisecond)
			if d.Runtime != nil {
				d.Runtime.StopAll(context.Background())
			}
			_ = identity.Clear(d.Opts.Paths)
			mgr := service.New()
			_ = mgr.Stop()
			d.cancelMu.Lock()
			if d.cancel != nil {
				d.cancel()
			}
			d.cancelMu.Unlock()
			time.Sleep(300 * time.Millisecond)
			os.Exit(0)
		}()
		return nil
	}
	return nil
}

func (d *Daemon) setActiveSession(sess *grpcclient.Session) {
	d.activeSessMu.Lock()
	defer d.activeSessMu.Unlock()
	d.activeSess = sess
}

func (d *Daemon) getActiveSession() *grpcclient.Session {
	d.activeSessMu.Lock()
	defer d.activeSessMu.Unlock()
	return d.activeSess
}

func (d *Daemon) resolveInquiry(inquiryID, optionID string) {
	d.inqMu.Lock()
	ch, ok := d.pendingInquiries[inquiryID]
	if ok {
		delete(d.pendingInquiries, inquiryID)
	}
	d.inqMu.Unlock()
	if ok && ch != nil {
		select {
		case ch <- optionID:
		default:
		}
	}
}

func (d *Daemon) handleAgentInquiry(ctx context.Context, jobID, empID string, inq acp.Inquiry) (string, error) {
	sess := d.getActiveSession()
	if sess == nil {
		return inq.DefaultOptionID(), nil
	}
	inquiryID := fmt.Sprintf("INQ-%s-%d", jobID, time.Now().UnixNano())
	ch := make(chan string, 1)

	d.inqMu.Lock()
	d.pendingInquiries[inquiryID] = ch
	d.inqMu.Unlock()

	defer func() {
		d.inqMu.Lock()
		delete(d.pendingInquiries, inquiryID)
		d.inqMu.Unlock()
	}()

	payload, _ := json.Marshal(map[string]any{
		"inquiry_id":  inquiryID,
		"job_id":      jobID,
		"employee_id": empID,
		"message":     inq.Message,
		"options":     inq.Options,
	})
	ev := d.newEvent(jobID, inq.SessionID, empID, aiev1.EventType_EVENT_TYPE_JOB_INQUIRY, string(payload))
	if err := sess.EnqueueEvent(ev); err != nil {
		return inq.DefaultOptionID(), nil
	}

	select {
	case <-ctx.Done():
		return inq.DefaultOptionID(), ctx.Err()
	case <-time.After(15 * time.Minute):
		return inq.DefaultOptionID(), fmt.Errorf("询问 %s 等待用户超时", inquiryID)
	case opt := <-ch:
		if opt == "" {
			return inq.DefaultOptionID(), nil
		}
		return opt, nil
	}
}

func (d *Daemon) handleIPC(ctx context.Context, req ipc.Request) ipc.Response {
	switch req.Method {
	case "ping":
		return okResult(map[string]any{"pong": true, "ready": d.Ready})
	case "status":
		e, w, s, j := d.Runtime.Snapshot()
		return okResult(map[string]any{
			"ready":        d.Ready,
			"version":      config.Version,
			"employees":    e,
			"workspaces":   w,
			"sessions":     s,
			"jobs":         j,
			"reconnect":    d.Backoff.State(),
			"cpu_percent":  d.Sample.CPUPercent,
			"mem_percent":  d.Sample.MemoryPercent,
			"disk_percent": d.Sample.DiskPercent,
			"providers":    d.Registry.List(),
		})
	case "doctor":
		return d.doctor()
	case "employee.ensure":
		var p struct{ ID, Name, Provider string }
		_ = json.Unmarshal(req.Params, &p)
		e, err := d.Runtime.EnsureEmployee(p.ID, p.Name, p.Provider)
		if err != nil {
			return errResp(err)
		}
		return okResult(e)
	case "workspace.ensure":
		var p struct{ ID, EmployeeID, Path string }
		_ = json.Unmarshal(req.Params, &p)
		w, err := d.Runtime.EnsureWorkspace(p.ID, p.EmployeeID, p.Path)
		if err != nil {
			return errResp(err)
		}
		return okResult(w)
	case "session.start":
		var p struct{ ID, EmployeeID, WorkspaceID, Provider string }
		_ = json.Unmarshal(req.Params, &p)
		s, err := d.Runtime.StartSession(ctx, p.ID, p.EmployeeID, p.WorkspaceID, p.Provider)
		if err != nil {
			return errResp(err)
		}
		return okResult(s)
	case "session.stop":
		var p struct{ ID string }
		_ = json.Unmarshal(req.Params, &p)
		if err := d.Runtime.StopSession(ctx, p.ID); err != nil {
			return errResp(err)
		}
		return okResult(map[string]string{"status": "stopped"})
	case "job.run":
		var p struct{ ID, EmployeeID, SessionID, Prompt string }
		_ = json.Unmarshal(req.Params, &p)
		j, reply, err := d.Runtime.RunJob(ctx, p.ID, p.EmployeeID, p.SessionID, p.Prompt)
		if err != nil {
			return errResp(err)
		}
		return okResult(map[string]any{"job": j, "reply": reply})
	case "provider.detect":
		var p struct{ Name string }
		_ = json.Unmarshal(req.Params, &p)
		prov, err := d.Registry.Get(p.Name)
		if err != nil {
			return errResp(err)
		}
		info, err := prov.Detect(ctx)
		if err != nil {
			return errResp(err)
		}
		return okResult(info)
	case "recovery.check":
		var p struct{ SessionID, JobID string }
		_ = json.Unmarshal(req.Params, &p)
		crashed, err := d.Recovery.CheckProcessCrash(ctx, p.SessionID, p.JobID)
		if err != nil {
			return errResp(err)
		}
		return okResult(map[string]any{"crashed": crashed})
	default:
		return ipc.Response{OK: false, Error: "未知方法: " + req.Method}
	}
}

func (d *Daemon) doctor() ipc.Response {
	checks := []map[string]string{}
	add := func(name, status, detail string) {
		checks = append(checks, map[string]string{"name": name, "status": status, "detail": detail})
	}
	if _, err := identity.Load(d.Opts.Paths); err != nil {
		add("identity", "WARN", err.Error())
	} else {
		add("identity", "OK", "已注册")
	}
	cfgPath := filepath.Join(d.Opts.Paths.ConfigDir(), "config.yaml")
	if _, err := os.Stat(cfgPath); err != nil {
		add("config", "WARN", "使用默认配置")
	} else {
		add("config", "OK", cfgPath)
	}
	for _, name := range d.Registry.List() {
		p, _ := d.Registry.Get(name)
		info, _ := p.Detect(context.Background())
		if info != nil && info.Path != "" {
			detail := info.Path
			status := "OK"
			if name == "cursor" {
				loggedIn := false
				email := ""
				home, _ := os.UserHomeDir()
				cfgCandidates := []string{filepath.Join(home, ".cursor", "cli-config.json")}
				sysDrive := os.Getenv("SystemDrive")
				if sysDrive == "" {
					sysDrive = "C:"
				}
				if entries, rerr := os.ReadDir(filepath.Join(sysDrive, "\\Users")); rerr == nil {
					for _, entry := range entries {
						if entry.IsDir() && entry.Name() != "Public" && entry.Name() != "Default" && entry.Name() != "All Users" {
							cfgCandidates = append(cfgCandidates, filepath.Join(sysDrive, "\\Users", entry.Name(), ".cursor", "cli-config.json"))
						}
					}
				}
				for _, cf := range cfgCandidates {
					if data, rerr := os.ReadFile(cf); rerr == nil {
						var parsed struct {
							AuthInfo struct {
								Email string `json:"email"`
							} `json:"authInfo"`
						}
						if json.Unmarshal(data, &parsed) == nil && parsed.AuthInfo.Email != "" {
							loggedIn = true
							email = parsed.AuthInfo.Email
							break
						}
					}
				}

				if loggedIn {
					status = "OK"
					detail = fmt.Sprintf("%s (已登录授权: %s)", info.Path, email)
				} else {
					checkCtx, checkCancel := context.WithTimeout(context.Background(), 8*time.Second)
					cmd := exec.CommandContext(checkCtx, info.Path, "status")
					if len(cfgCandidates) > 0 {
						targetHome := filepath.Dir(filepath.Dir(cfgCandidates[len(cfgCandidates)-1]))
						cmd.Env = append(os.Environ(),
							"USERPROFILE="+targetHome,
							"HOME="+targetHome,
							"LOCALAPPDATA="+filepath.Join(targetHome, "AppData", "Local"),
							"APPDATA="+filepath.Join(targetHome, "AppData", "Roaming"),
						)
					}
					out, err := cmd.CombinedOutput()
					checkCancel()
					outStr := string(out)
					if err == nil && strings.Contains(outStr, "Logged in as") {
						status = "OK"
						detail = fmt.Sprintf("%s (%s)", info.Path, strings.TrimSpace(outStr))
					} else {
						status = "WARN"
						detail = fmt.Sprintf("%s (未登录授权，请在终端执行 'agent login')", info.Path)
					}
				}
			} else if name == "antigravity" {
				home, _ := os.UserHomeDir()
				daemonDir := filepath.Join(home, ".gemini", "antigravity", "daemon")
				entries, _ := os.ReadDir(daemonDir)
				hasDaemon := false
				for _, e := range entries {
					if strings.HasPrefix(e.Name(), "ls_") && strings.HasSuffix(e.Name(), ".json") {
						hasDaemon = true
						break
					}
				}
				if hasDaemon {
					status = "OK"
					detail = fmt.Sprintf("%s (LanguageServer 运行中)", info.Path)
				} else {
					status = "OK"
					detail = fmt.Sprintf("%s (已检测到安装)", info.Path)
				}
			}
			add("provider."+name, status, detail)
		} else {
			add("provider."+name, "WARN", "未检测到本机安装（可用 Fake ACP）")
		}
	}
	if d.Ready {
		add("daemon", "OK", "ready")
	} else {
		add("daemon", "FAIL", "not ready")
	}
	return okResult(map[string]any{"checks": checks})
}

func (d *Daemon) installedProviders(ctx context.Context) []string {
	if d.Registry == nil {
		return nil
	}
	var out []string
	for _, name := range d.Registry.List() {
		prov, err := d.Registry.Get(name)
		if err != nil {
			continue
		}
		info, err := prov.Detect(ctx)
		if err != nil || info == nil || info.Path == "" {
			continue
		}
		base := strings.ToLower(filepath.Base(info.Path))
		if strings.Contains(base, "fake") {
			continue
		}
		out = append(out, name)
	}
	return out
}

// promptWithMarkdownDefault 文本回复默认 Markdown，用户明确要求其他格式时仍以用户要求为准。
func promptWithMarkdownDefault(prompt string) string {
	const note = "【输出格式】文本回复与任务制品默认使用 Markdown（标题、列表、表格、代码块）。仅当用户明确要求纯文本、JSON、HTML 或其他格式时，才按该要求输出。"
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return note
	}
	return note + "\n\n" + prompt
}

// reportModels 向控制面上报一次模型列表。注册连上时调用，或响应刷新命令。不进入心跳周期。
func (d *Daemon) reportModels(ctx context.Context) {
	d.modelMu.Lock()
	if d.modelRefreshing {
		d.modelAgain = true
		d.modelMu.Unlock()
		return
	}
	d.modelRefreshing = true
	d.modelMu.Unlock()

	for {
		d.collectAndSendModels(ctx)
		d.modelMu.Lock()
		again := d.modelAgain
		d.modelAgain = false
		if !again {
			d.modelRefreshing = false
			d.modelMu.Unlock()
			return
		}
		d.modelMu.Unlock()
	}
}

func (d *Daemon) collectAndSendModels(ctx context.Context) {
	if d.Registry == nil {
		return
	}
	d.modelMu.Lock()
	cached := append([]heartbeat.ModelInfo(nil), d.modelCache...)
	d.modelMu.Unlock()

	byProv := map[string][]heartbeat.ModelInfo{}
	for _, item := range cached {
		byProv[item.Provider] = append(byProv[item.Provider], item)
	}
	cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	for _, name := range d.Registry.List() {
		prov, err := d.Registry.Get(name)
		if err != nil {
			continue
		}
		lister, ok := prov.(interface {
			ListModels(context.Context) ([]providers.Model, error)
		})
		if !ok {
			continue
		}
		models, err := lister.ListModels(cctx)
		if err != nil {
			continue
		}
		items := make([]heartbeat.ModelInfo, 0, len(models))
		for _, m := range models {
			if m.ID == "" {
				continue
			}
			label := m.Label
			if label == "" {
				label = m.ID
			}
			items = append(items, heartbeat.ModelInfo{Provider: name, ID: m.ID, Label: label})
		}
		byProv[name] = items
	}
	var out []heartbeat.ModelInfo
	for _, name := range d.Registry.List() {
		out = append(out, byProv[name]...)
	}
	if len(out) == 0 {
		return
	}
	d.modelMu.Lock()
	d.modelCache = out
	d.modelMu.Unlock()

	sess := d.getActiveSession()
	if sess == nil || d.Runtime == nil {
		return
	}
	e, _, s, _ := d.Runtime.Snapshot()
	sample := d.Sample
	if sample.CPUPercent == 0 && sample.MemoryPercent == 0 && d.Monitor != nil {
		sample = d.Monitor.Sample()
	}
	_ = sess.SendHeartbeat(context.Background(), heartbeat.Stats{
		Employees: uint32(e),
		Sessions:  uint32(s),
		CPU:       sample.CPUPercent,
		Memory:    sample.MemoryPercent,
		Disk:      sample.DiskPercent,
		Providers: d.installedProviders(context.Background()),
		Models:    out,
	})
}

func okResult(v any) ipc.Response {
	b, _ := json.Marshal(v)
	return ipc.Response{OK: true, Result: b}
}

func errResp(err error) ipc.Response {
	return ipc.Response{OK: false, Error: err.Error()}
}

// stageJobArtifact 本地 Stage 并尽力上传（失败保留队列）。
func (d *Daemon) stageJobArtifact(jobID, name, typ string, data []byte) {
	if d.Artifacts == nil || len(data) == 0 {
		return
	}
	if d.ArtUpload != nil {
		_, _ = d.ArtUpload.StageAndTryUpload(jobID, name, typ, data)
		return
	}
	_, _ = d.Artifacts.Stage(jobID, name, typ, data)
}

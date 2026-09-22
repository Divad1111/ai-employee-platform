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
	"strings"
	"sync/atomic"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
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
	if b, err := identity.Load(opts.Paths); err == nil && b != nil {
		d.wsID = b.WorkstationID
	}
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
			}
		}
		sess.OnCommand = func(cctx context.Context, cmd *aiev1.Command) error {
			return d.handleCommand(cctx, sess, cmd)
		}
		d.Backoff.MarkConnected()
		_ = sess.Run(ctx)
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

		// 确保本地员工视图
		_, _ = d.Runtime.EnsureEmployee(empID, empID, "cursor")

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
		if activeSess != nil && len(mcpServers) == 0 {
			sessID = activeSess.ID
		} else {
			if activeSess != nil {
				_ = d.Runtime.StopSession(ctx, activeSess.ID)
			}
			sessID = "ses-" + empID
			_ = sess.EnqueueEvent(d.newEvent(jobID, sessID, empID, aiev1.EventType_EVENT_TYPE_SESSION_STARTED,
				fmt.Sprintf(`{"status":"STARTING","provider":"cursor","workspace_id":%q}`, wsID)))
			startCtx, startCancel := context.WithTimeout(ctx, 2*time.Minute)
			_, err := d.Runtime.StartSessionWithMCP(startCtx, sessID, empID, wsID, "cursor", mcpServers)
			startCancel()
			if err != nil && err != runtime.ErrActiveSession {
				_ = sess.EnqueueEvent(d.newEvent(jobID, sessID, empID, aiev1.EventType_EVENT_TYPE_SESSION_ERROR,
					fmt.Sprintf(`{"status":"ERROR","error":%q}`, err.Error())))
				_ = sess.EnqueueEvent(d.newEvent(jobID, sessID, empID, aiev1.EventType_EVENT_TYPE_JOB_FAILED,
					fmt.Sprintf(`{"error":%q,"session_id":%q}`, err.Error(), sessID)))
				return err
			}
			_ = sess.EnqueueEvent(d.newEvent(jobID, sessID, empID, aiev1.EventType_EVENT_TYPE_SESSION_READY,
				fmt.Sprintf(`{"status":"READY","provider":"cursor","workspace_id":%q}`, wsID)))
		}

		_ = sess.EnqueueEvent(d.newEvent(jobID, sessID, empID, aiev1.EventType_EVENT_TYPE_JOB_STARTED,
			fmt.Sprintf(`{"status":"running","session_id":%q}`, sessID)))

		go func() {
			jobCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			_, reply, rerr := d.Runtime.RunJob(jobCtx, jobID, empID, sessID, payload.Prompt)
			if rerr != nil {
				pl, _ := json.Marshal(map[string]string{
					"error": rerr.Error(), "session_id": sessID, "reply": reply,
				})
				_ = sess.EnqueueEvent(d.newEvent(jobID, sessID, empID, aiev1.EventType_EVENT_TYPE_JOB_FAILED, string(pl)))
				return
			}
			pl, _ := json.Marshal(map[string]string{
				"status": "success", "session_id": sessID, "reply": reply, "message": "Job executed successfully",
			})
			_ = sess.EnqueueEvent(d.newEvent(jobID, sessID, empID, aiev1.EventType_EVENT_TYPE_JOB_SUCCESS, string(pl)))
			// Job 结束后会话回到 READY（可复用）
			_ = sess.EnqueueEvent(d.newEvent(jobID, sessID, empID, aiev1.EventType_EVENT_TYPE_SESSION_READY,
				`{"status":"READY"}`))
		}()
		return nil

	case aiev1.CommandType_COMMAND_TYPE_STOP_JOB:
		d.Runtime.MarkUnknown("", cmd.GetJobId())
		return nil
	}
	return nil
}

func (d *Daemon) handleIPC(ctx context.Context, req ipc.Request) ipc.Response {
	switch req.Method {
	case "ping":
		return okResult(map[string]any{"pong": true, "ready": d.Ready})
	case "status":
		e, w, s, j := d.Runtime.Snapshot()
		return okResult(map[string]any{
			"ready":       d.Ready,
			"version":     config.Version,
			"employees":   e,
			"workspaces":  w,
			"sessions":    s,
			"jobs":        j,
			"reconnect":   d.Backoff.State(),
			"cpu_percent": d.Sample.CPUPercent,
			"mem_percent": d.Sample.MemoryPercent,
			"disk_percent": d.Sample.DiskPercent,
			"providers":   d.Registry.List(),
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

func okResult(v any) ipc.Response {
	b, _ := json.Marshal(v)
	return ipc.Response{OK: true, Result: b}
}

func errResp(err error) ipc.Response {
	return ipc.Response{OK: false, Error: err.Error()}
}

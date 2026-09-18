// Package daemon 实现 aew daemon 启动流水线。
// Load Config → Identity → Store/Recover → IPC → Connect → Heartbeat → Ready。
// 设计依据：设计文档 §123、§68、§89。
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/workstation/internal/config"
	"github.com/ai-employee-platform/workstation/internal/controlplane/ack"
	grpcclient "github.com/ai-employee-platform/workstation/internal/controlplane/grpc"
	"github.com/ai-employee-platform/workstation/internal/controlplane/outbox"
	"github.com/ai-employee-platform/workstation/internal/controlplane/reconnect"
	"github.com/ai-employee-platform/workstation/internal/identity"
	"github.com/ai-employee-platform/workstation/internal/ipc"
	"github.com/ai-employee-platform/workstation/internal/monitor"
	"github.com/ai-employee-platform/workstation/internal/platform"
	"github.com/ai-employee-platform/workstation/internal/providers"
	"github.com/ai-employee-platform/workstation/internal/providers/codex"
	"github.com/ai-employee-platform/workstation/internal/providers/cursor"
	"github.com/ai-employee-platform/workstation/internal/runtime"
	"github.com/ai-employee-platform/workstation/internal/runtime/process"
	"github.com/ai-employee-platform/workstation/internal/runtime/recovery"
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
	proc := process.NewManager("cursor", "cursor.exe", "Cursor", "codex", "codex.exe", "cursor-fake", "codex-fake")
	reg := providers.NewRegistry()
	curPath, codPath := "", ""
	if opts.Config.Providers != nil {
		if p, ok := opts.Config.Providers["cursor"]; ok {
			curPath = p.Path
		}
		if p, ok := opts.Config.Providers["codex"]; ok {
			codPath = p.Path
		}
	}
	reg.Register(cursor.NewProvider(proc, curPath))
	reg.Register(codex.NewProvider(proc, codPath))
	rt := runtime.NewManagers(opts.Paths, reg)
	rt.MaxSess = opts.Config.MaxSessions
	ob := outbox.NewMemoryStore()
	rt.Sink = func(typ string, payload map[string]string) {
		// 本地事件经 Outbox 结构落盘语义：以 payload JSON 作为占位 Event
		id := typ + "-" + time.Now().Format("150405.000")
		_ = ob.Enqueue(localEvent(id, typ, payload))
	}
	return &Daemon{
		Opts:     opts,
		Runtime:  rt,
		Proc:     proc,
		Registry: reg,
		Recovery: &recovery.Manager{Runtime: rt, Proc: proc},
		Monitor:  &monitor.Sampler{},
		Outbox:   ob,
		Backoff:  reconnect.NewBackoff(60 * time.Second),
	}
}

func localEvent(id, typ string, payload map[string]string) *aiev1.Event {
	b, _ := json.Marshal(payload)
	return &aiev1.Event{
		EventId: id,
		Type:    aiev1.EventType_EVENT_TYPE_SYSTEM_ALERT,
		Meta: &aiev1.EnvelopeMeta{
			MessageId: id, Sequence: uint64(time.Now().UnixNano()),
			TimestampUnixMs: time.Now().UnixMilli(),
		},
		PayloadJson: string(b),
	}
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
		j, err := d.Runtime.RunJob(ctx, p.ID, p.EmployeeID, p.SessionID, p.Prompt)
		if err != nil {
			return errResp(err)
		}
		return okResult(j)
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
			add("provider."+name, "OK", info.Path)
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

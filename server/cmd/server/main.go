// Package main 是 Control Plane 服务进程入口。
// 启动 Admin REST / SSE / Feishu Webhook 与 Workstation mTLS gRPC。
// 设计依据：设计文档 §4–§5、§20、§78。
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	// 时区数据编进二进制。运行镜像没有 tzdata 时，Asia/Shanghai 会加载失败并按 UTC 计时。
	_ "time/tzdata"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/approval"
	"github.com/ai-employee-platform/server/internal/artifact"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/automation"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/config"
	"github.com/ai-employee-platform/server/internal/database"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/enrollment"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/mcpauth"
	"github.com/ai-employee-platform/server/internal/mcpserver"
	"github.com/ai-employee-platform/server/internal/message"
	"github.com/ai-employee-platform/server/internal/metrics"
	"github.com/ai-employee-platform/server/internal/notification"
	"github.com/ai-employee-platform/server/internal/permission"
	"github.com/ai-employee-platform/server/internal/quota"
	"github.com/ai-employee-platform/server/internal/registry"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/scheduler"
	"github.com/ai-employee-platform/server/internal/secret"
	"github.com/ai-employee-platform/server/internal/session"
	"github.com/ai-employee-platform/server/internal/workergrpc"
	"github.com/ai-employee-platform/server/internal/workflowmcp"
	"github.com/ai-employee-platform/server/internal/workspace"
	"github.com/ai-employee-platform/server/internal/workstation"
	"github.com/ai-employee-platform/server/internal/wsmember"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fatal("加载配置失败: %v", err)
	}

	auditor := audit.NewMemory()
	bus := eventbus.New(500)

	var (
		users       auth.UserStore        = auth.NewMemoryUserStore()
		webSessions auth.SessionStore     = auth.NewMemorySessionStore()
		empStore    employee.Store        = employee.NewMemoryStore()
		wsStore     workspace.Store       = workspace.NewMemoryStore()
		sessStore   session.Store         = session.NewMemoryStore()
		jobStore    job.Store             = job.NewMemoryStore()
		wsMetaStore workstation.MetaStore = workstation.NewMemoryMeta()
		totpStore   approval.TOTPStore    = approval.NewMemoryTOTP()
		certStore   certca.CertificateStore
		wfStore     workflowmcp.Store = workflowmcp.NewMemoryStore()
		mcpTokStore mcpauth.Store     = mcpauth.NewMemoryStore()
		autoStore   automation.Store  = automation.NewMemoryStore()
		artStore    artifact.Store    = artifact.NewMemoryStore()
		wsMembers   wsmember.Store    = wsmember.NewMemoryStore()
		quotaStore  quota.Store       = quota.NewMemoryStore()
		pgSQL       *sql.DB
	)

	if cfg.DatabaseURL != "" {
		var db *database.DB
		var err error
		// Postgres 与本进程同时拉起时，第一次 ping 会拒绝连接。空库回退会让管理页误进首次设置。
		for attempt := 1; attempt <= 30; attempt++ {
			db, err = database.Open(cfg.DatabaseURL)
			if err == nil {
				break
			}
			fmt.Printf("⚠️ 连接 PostgreSQL 失败 (%v)，%d/30 秒后重试\n", err, attempt)
			time.Sleep(time.Second)
		}
		if err != nil {
			fmt.Printf("⚠️ 连接 PostgreSQL 仍失败 (%v)，回退到内存存储\n", err)
		} else {
			fmt.Println("✅ 数据库: 已连接 PostgreSQL，启用全量持久化 (Users, Employees, Workspaces, Jobs, Sessions, Workstations, TOTP, Certificates, WorkflowMCP, Automation, Artifacts)")
			defer db.Close()
			pgSQL = db.SQL
			users = db.NewUserStore()
			webSessions = db.NewWebSessionStore()
			empStore = db.NewEmployeeStore()
			wsStore = db.NewWorkspaceStore()
			sessStore = db.NewSessionStore()
			jobStore = db.NewJobStore()
			wsMetaStore = db.NewWorkstationMetaStore()
			totpStore = db.NewTOTPStore()
			certStore = db.NewCertStore()
			wfStore = workflowmcp.NewPostgresStore(db.SQL)
			mcpTokStore = mcpauth.NewPostgresStore(db.SQL)
			autoStore = automation.NewPostgresStore(db.SQL)
			artStore = artifact.NewPostgresStore(db.SQL)
			wsMembers = db.NewWSMemberStore()
			quotaStore = quota.NewPostgresStore(db.SQL)
		}
	}

	if getenv("AIE_DEV_SEED_ADMIN", "0") == "1" {
		adminUser := getenv("AIE_ADMIN_USER", "admin")
		adminPass := getenv("AIE_ADMIN_PASSWORD", "admin123")
		if seeder, ok := users.(interface{ SeedAdmin(u, p, d string) error }); ok {
			_ = seeder.SeedAdmin(adminUser, adminPass, "Administrator")
		}
	}
	authSvc := auth.NewService(users, webSessions, auditor)

	caDir := getenv("AIE_CA_DIR", "/var/lib/aie/ca")
	ca, err := certca.LoadOrNewAuthority(caDir)
	if err != nil {
		fatal("初始化 CA 失败: %v", err)
	}
	if certStore != nil {
		ca.SetStore(certStore)
	}
	enrollSvc := enrollment.NewService(enrollment.NewMemoryStore(), ca, auditor)

	var vault secret.Store
	mv, err := secret.NewMemoryVault()
	if err != nil {
		fatal("初始化 Secret Vault 失败: %v", err)
	}
	vault = mv
	// 若设置 AIE_SECRET_DIR，改用文件加密 Vault（可插拔 Prod 路径）
	if dir := getenv("AIE_SECRET_DIR", ""); dir != "" {
		fv, err := secret.NewFileVault(dir)
		if err != nil {
			fatal("初始化 FileVault 失败: %v", err)
		}
		vault = fv
	}

	presence := reliability.NewPresence(5, 15)
	empSvc := employee.NewService(empStore, auditor, bus)
	wsSvc := workspace.NewService(wsStore, auditor)
	wsSvc.SetBinder(workspace.EmployeeBridge{
		GetByWorkspace: func(ctx context.Context, wsID string) (string, error) {
			e, err := empStore.FindByWorkspace(ctx, wsID)
			if err != nil || e == nil {
				return "", err
			}
			return e.ID, nil
		},
		SetWorkspace: func(ctx context.Context, empID, wsID string) error {
			e, err := empStore.Get(ctx, empID)
			if err != nil {
				return err
			}
			e.WorkspaceID = wsID
			e.UpdatedAt = time.Now().UTC()
			return empStore.Save(ctx, e)
		},
	})
	wsNodeSvc := workstation.NewService(ca, presence, wsMetaStore)
	sessSvc := session.NewService(sessStore, auditor, bus)
	jobSvc := job.NewService(jobStore, auditor, bus)
	msgSvc := message.NewService(message.NewMemoryStore(), auditor)

	feishuSvc := feishu.NewService(vault)
	notifySvc := notification.New(feishuSvc, bus, jobSvc)

	// 启动飞书官方 WebSocket 长连接网关（若已配置并启用）
	if feishuSvc.Gateway != nil {
		_ = feishuSvc.Gateway.Restart(context.Background())
	}

	presence.OnOffline = func(wsID string) {
		bus.Publish(context.Background(), eventbus.TypeWorkstationOffline, map[string]string{"workstation_id": wsID})
		list, _ := jobSvc.List(context.Background())
		for _, j := range list {
			if j.WorkstationID == wsID && j.Status == job.StatusRunning {
				uj, err := jobSvc.MarkUnknown(context.Background(), j.ID, "system", "")
				if err == nil {
					_ = notifySvc.OnJobTerminal(context.Background(), uj)
				}
			}
		}
	}

	srvCert, err := workergrpc.LoadServerCertificate(ca, "localhost", "127.0.0.1")
	if err != nil {
		fatal("签发服务端证书失败: %v", err)
	}
	gs, lis, workerSvc, err := workergrpc.ListenAndServeWith(cfg.GRPCAddr, ca, srvCert, workergrpc.Options{
		CA: ca, Presence: presence,
	})
	if err != nil {
		fatal("启动 gRPC 失败: %v", err)
	}
	defer gs.GracefulStop()
	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	defer sweepCancel()
	workerSvc.StartPresenceSweeper(sweepCtx, time.Second)

	sched := scheduler.New(jobSvc, empSvc, wsNodeSvc, presence, workerSvc)
	sched.SetWorkspaces(wsSvc)

	quotaSvc := quota.NewService(quotaStore)
	autoSvc := automation.New(autoStore, jobSvc, sched, vault, auditor)
	autoSvc.SetNotify(automation.FeishuBridge{Svc: feishuSvc}, notifySvc)
	sched.OnTerminal = func(ctx context.Context, j *job.Job) {
		_ = notifySvc.OnJobTerminal(ctx, j)
		autoSvc.OnJobTerminal(ctx, j)
	}
	autoSvc.Quota = quotaSvc
	autoSvc.Roles = func(ctx context.Context, userID string) []string {
		u, err := users.FindByID(ctx, userID)
		if err != nil || u == nil {
			return nil
		}
		return u.Roles
	}

	workerSvc.OnEvent = func(wsID string, ev *aiev1.Event) {
		ctx := context.Background()
		jobID := ev.GetJobId()
		sessID := ev.GetSessionId()
		empID := ev.GetEmployeeId()
		var payload map[string]string
		if pJSON := ev.GetPayloadJson(); pJSON != "" {
			_ = json.Unmarshal([]byte(pJSON), &payload)
		}
		if payload == nil {
			payload = make(map[string]string)
		}
		if sessID == "" {
			sessID = payload["session_id"]
		}
		if empID == "" {
			empID = payload["employee_id"]
		}
		if empID == "" && jobID != "" {
			if j, err := jobSvc.Get(ctx, jobID); err == nil && j != nil {
				empID = j.EmployeeID
			}
		}

		switch ev.GetType() {
		case aiev1.EventType_EVENT_TYPE_SESSION_STARTED,
			aiev1.EventType_EVENT_TYPE_SESSION_READY,
			aiev1.EventType_EVENT_TYPE_SESSION_ERROR,
			aiev1.EventType_EVENT_TYPE_SESSION_STOPPED:
			st := payload["status"]
			if st == "" {
				switch ev.GetType() {
				case aiev1.EventType_EVENT_TYPE_SESSION_STARTED:
					st = session.StatusStarting
				case aiev1.EventType_EVENT_TYPE_SESSION_READY:
					st = session.StatusReady
				case aiev1.EventType_EVENT_TYPE_SESSION_ERROR:
					st = session.StatusError
				case aiev1.EventType_EVENT_TYPE_SESSION_STOPPED:
					st = session.StatusStopped
				}
			}
			if sessID != "" && empID != "" {
				_, _ = sessSvc.ReportFromWorkstation(ctx, sessID, empID, wsID, payload["workspace_id"], payload["provider"], st)
			}
			if jobID != "" {
				_ = jobSvc.AppendEvent(ctx, jobID, "SESSION", map[string]string{
					"session_id": sessID, "status": st, "event": ev.GetType().String(),
				})
				if sessID != "" {
					_ = jobSvc.BindSession(ctx, jobID, sessID)
				}
			}

		case aiev1.EventType_EVENT_TYPE_JOB_STARTED:
			if jobID != "" {
				if sessID != "" {
					_ = jobSvc.BindSession(ctx, jobID, sessID)
					_, _ = sessSvc.ReportFromWorkstation(ctx, sessID, empID, wsID, payload["workspace_id"], payload["provider"], session.StatusBusy)
				}
				_, _ = jobSvc.Transition(ctx, jobID, job.StatusRunning, "workstation", wsID, payload)
			}
		case aiev1.EventType_EVENT_TYPE_JOB_SUCCESS:
			if jobID != "" {
				if reply := payload["reply"]; reply != "" {
					_ = jobSvc.SetResult(ctx, jobID, reply)
					_ = jobSvc.AppendEvent(ctx, jobID, "AGENT_REPLY", map[string]string{"reply": reply})
				}
				uj, err := jobSvc.Transition(ctx, jobID, job.StatusSuccess, "workstation", wsID, payload)
				sched.Release(wsID)
				if err != nil {
					fmt.Printf("[Server] ⚠️ Transition to SUCCESS 失败 (job=%s): %v\n", jobID, err)
				}
				if err == nil && uj != nil {
					if uj.Result == "" && payload["reply"] != "" {
						uj.Result = payload["reply"]
					}
					recordJobTokens(ctx, jobSvc, quotaStore, jobID, payload)
					_ = notifySvc.OnJobTerminal(ctx, uj)
					autoSvc.OnJobTerminal(ctx, uj)
				}
			}
		case aiev1.EventType_EVENT_TYPE_JOB_FAILED:
			if jobID != "" {
				if reply := payload["reply"]; reply != "" {
					_ = jobSvc.SetResult(ctx, jobID, reply)
					_ = jobSvc.AppendEvent(ctx, jobID, "AGENT_REPLY", map[string]string{"reply": reply, "partial": "true"})
				}
				uj, err := jobSvc.Transition(ctx, jobID, job.StatusFailed, "workstation", wsID, payload)
				sched.Release(wsID)
				if err != nil {
					fmt.Printf("[Server] ⚠️ Transition to FAILED 失败 (job=%s): %v\n", jobID, err)
				}
				if err == nil && uj != nil {
					recordJobTokens(ctx, jobSvc, quotaStore, jobID, payload)
					_ = notifySvc.OnJobTerminal(ctx, uj)
					autoSvc.OnJobTerminal(ctx, uj)
				}
			}
		}
	}
	permStore := permission.NewMemoryStore()
	_ = permission.EnsureDefault(permStore)
	permEng := permission.NewEngine(permStore, auditor)
	secretMgr := secret.NewManager(vault, secret.NewMemoryBindings(), auditor)
	approvalSvc := approval.New(approval.NewMemoryStore(), totpStore, vault, jobSvc, permEng, auditor, bus)
	artRoot := getenv("AIE_ARTIFACT_DIR", "")
	if artRoot == "" {
		secretDir := getenv("AIE_SECRET_DIR", "/data/secrets")
		artRoot = filepath.Join(filepath.Dir(secretDir), "artifacts")
	}
	_ = os.MkdirAll(artRoot, 0o750)
	artSvc := artifact.New(artStore, artRoot)
	artSvc.SetJobLookup(artifactJobLookup{jobs: jobSvc})
	_ = pgSQL // 保留连接引用供未来扩展；制品已走 artStore
	regSvc, _, err := registry.New(registry.NewMemoryStore(), getenv("AIE_PROVIDER_SIGNING_PUBKEY", ""))
	if err != nil {
		fatal("初始化 Provider Registry 失败: %v", err)
	}
	_ = regSvc.UpsertProvider(context.Background(), &registry.Provider{
		ID: "cursor", Name: "Cursor", Capabilities: map[string]bool{"acp": true, "headless": true},
	})
	_ = regSvc.UpsertProvider(context.Background(), &registry.Provider{
		ID: "codex", Name: "Codex", Capabilities: map[string]bool{"acp": true},
	})
	met := metrics.New()
	wfSvc := workflowmcp.NewService(wfStore)
	mcpAuthSvc := mcpauth.NewService(mcpTokStore)
	bridge := &feishu.Bridge{
		Employees: empSvc, Jobs: jobSvc, Messages: msgSvc,
		Scheduler: sched, Notify: notifySvc, Feishu: feishuSvc,
		Quota: quotaSvc,
		Roles: func(ctx context.Context, userID string) []string {
			u, err := users.FindByID(ctx, userID)
			if err != nil || u == nil {
				return nil
			}
			return u.Roles
		},
	}
	bridge.Wire()

	mcpURL := getenv("AIE_MCP_PUBLIC_URL", "http://127.0.0.1"+cfg.HTTPAddr+"/mcp")
	if cfg.HTTPAddr != "" && cfg.HTTPAddr[0] == ':' {
		mcpURL = getenv("AIE_MCP_PUBLIC_URL", "http://127.0.0.1"+cfg.HTTPAddr+"/mcp")
	}
	sched.SetWorkflowMCP(wfSvc, mcpURL)
	sched.SetMCPAuth(mcpAuthSvc)
	sched.SetFullPusher(workerSvc)

	// 生产/真实运行模式：默认不注入演示数据，只使用实际接入的工作站与业务数据
	if getenv("AIE_SEED_DEMO", "0") == "1" && cfg.Env != "production" {
		fmt.Println("调试模式: 正在注入开发演示数据...")
		seedDemoData(empSvc, wsSvc, wsNodeSvc, presence, jobSvc, feishuSvc, wfSvc)
		go func() {
			t := time.NewTicker(5 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-sweepCtx.Done():
					return
				case <-t.C:
					presence.Touch("WS-DEMO-01", "0.0.1-dev", 1, 1, 32, 48, 55)
					presence.Touch("WS-DEMO-02", "0.0.1-dev", 0, 0, 71, 82, 40)
				}
			}
		}()
	}

	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-sweepCtx.Done():
				return
			case <-t.C:
				sched.Tick(context.Background())
				autoSvc.Tick(context.Background(), time.Now().UTC())
				// 刷新基础 metrics
				nOnline := 0
				for _, v := range wsNodeSvc.List(context.Background()) {
					if v.Status == reliability.StatusOnline {
						nOnline++
					}
				}
				met.WorkstationOnline.Store(int64(nOnline))
				if n, err := jobSvc.ActiveCount(context.Background()); err == nil {
					met.JobRunning.Store(int64(n))
				}
				if list, err := sessSvc.List(context.Background()); err == nil {
					var n int64
					for _, s := range list {
						if s.Status == "READY" || s.Status == "BUSY" || s.Status == "STARTING" {
							n++
						}
					}
					met.ActiveSessions.Store(n)
				}
			}
		}
	}()

	httpHandler := api.NewRouter(api.Deps{
		Auth:            authSvc,
		Enrollment:      enrollSvc,
		CA:              ca,
		Employees:       empSvc,
		Workspaces:      wsSvc,
		Workstations:    wsNodeSvc,
		Sessions:        sessSvc,
		Jobs:            jobSvc,
		Messages:        msgSvc,
		Bus:             bus,
		Audit:           auditor,
		Feishu:          feishuSvc,
		Scheduler:       sched,
		Notify:          notifySvc,
		Secrets:         vault,
		SecretMgr:       secretMgr,
		Permission:      permEng,
		PermissionStore: permStore,
		Approvals:       approvalSvc,
		Artifacts:       artSvc,
		Registry:        regSvc,
		Metrics:         met,
		WorkflowMCP:     wfSvc,
		MCPAuth:         mcpAuthSvc,
		SkillSyncer:     sched,
		Automation:      autoSvc,
		WSMembers:       wsMembers,
		Quota:           quotaSvc,
	})
	mcpSrv := &mcpserver.Server{WF: wfSvc, MCPAuth: mcpAuthSvc, Auth: authSvc, Syncer: sched}
	mux := http.NewServeMux()
	mux.Handle("/", httpHandler)
	mux.Handle("/mcp", mcpSrv.Handler())
	httpSrv := &http.Server{Addr: cfg.HTTPAddr, Handler: mux}
	go func() {
		fmt.Printf("HTTP 监听 %s\n", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "HTTP 错误: %v\n", err)
		}
	}()

	fmt.Printf("AI Employee Control Plane %s\n", config.Version)
	fmt.Printf("环境=%s gRPC(mTLS)=%s\n", cfg.Env, lis.Addr().String())
	if ok, _ := users.IsInitialized(context.Background()); ok {
		fmt.Println("系统认证: 已就绪 (已存在管理员)")
	} else {
		fmt.Println("系统认证: 尚未初始化，请访问 Web 完成首次部署设置 (http://localhost:8088/setup 或 POST /api/setup/init)")
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	_ = httpSrv.Close()
	fmt.Println("已关闭")
}

func recordJobTokens(ctx context.Context, jobSvc *job.Service, store quota.Store, jobID string, payload map[string]string) {
	inTok, _ := strconv.ParseInt(payload["input_tokens"], 10, 64)
	outTok, _ := strconv.ParseInt(payload["output_tokens"], 10, 64)
	src := payload["token_source"]
	if src == "" && inTok == 0 && outTok == 0 {
		return
	}
	userID, first, err := jobSvc.RecordTokens(ctx, jobID, inTok, outTok, payload["agent"], src)
	if err != nil || !first || store == nil {
		return
	}
	if userID == "" || userID == "feishu" || strings.HasPrefix(userID, "automation:") || strings.HasPrefix(userID, "feishu:") {
		return
	}
	_, _ = store.AddUsage(ctx, quota.TypeUser, userID, quota.PeriodMonthly, inTok+outTok, 1)
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// artifactJobLookup 将 job.Service 适配为 artifact.JobLookup。
type artifactJobLookup struct {
	jobs *job.Service
}

func (a artifactJobLookup) LookupJob(ctx context.Context, id string) (*artifact.JobInfo, error) {
	if a.jobs == nil {
		return nil, artifact.ErrJobRequired
	}
	j, err := a.jobs.Get(ctx, id)
	if err != nil || j == nil {
		return nil, artifact.ErrJobRequired
	}
	return &artifact.JobInfo{ID: j.ID, WorkstationID: j.WorkstationID, Status: j.Status}, nil
}

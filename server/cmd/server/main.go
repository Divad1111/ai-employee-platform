// Package main 是 Control Plane 服务进程入口。
// 启动 Admin REST / SSE / Feishu Webhook 与 Workstation mTLS gRPC。
// 设计依据：设计文档 §4–§5、§20、§78。
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/approval"
	"github.com/ai-employee-platform/server/internal/artifact"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/config"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/enrollment"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/knowledge"
	"github.com/ai-employee-platform/server/internal/message"
	"github.com/ai-employee-platform/server/internal/metrics"
	"github.com/ai-employee-platform/server/internal/notification"
	"github.com/ai-employee-platform/server/internal/permission"
	"github.com/ai-employee-platform/server/internal/registry"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/scheduler"
	"github.com/ai-employee-platform/server/internal/secret"
	"github.com/ai-employee-platform/server/internal/session"
	"github.com/ai-employee-platform/server/internal/skill"
	"github.com/ai-employee-platform/server/internal/workergrpc"
	"github.com/ai-employee-platform/server/internal/workstation"
	"github.com/ai-employee-platform/server/internal/workspace"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fatal("加载配置失败: %v", err)
	}

	auditor := audit.NewMemory()
	bus := eventbus.New(500)
	users := auth.NewMemoryUserStore()
	adminUser := getenv("AIE_ADMIN_USER", "admin")
	adminPass := getenv("AIE_ADMIN_PASSWORD", "admin123")
	if err := users.SeedAdmin(adminUser, adminPass, "Administrator"); err != nil {
		fatal("初始化管理员失败: %v", err)
	}
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), auditor)

	ca, err := certca.NewDevAuthority()
	if err != nil {
		fatal("初始化 CA 失败: %v", err)
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
	empStore := employee.NewMemoryStore()
	empSvc := employee.NewService(empStore, auditor, bus)
	wsStore := workspace.NewMemoryStore()
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
	wsNodeSvc := workstation.NewService(ca, presence, nil)
	sessSvc := session.NewService(session.NewMemoryStore(), auditor, bus)
	jobSvc := job.NewService(job.NewMemoryStore(), auditor, bus)
	msgSvc := message.NewService(message.NewMemoryStore(), auditor)

	feishuSender := &feishu.MemorySender{}
	feishuSvc := feishu.NewService(vault)
	feishuSvc.Sender = feishuSender
	notifySvc := notification.New(feishuSvc, bus, jobSvc)

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
	permStore := permission.NewMemoryStore()
	_ = permission.EnsureDefault(permStore)
	permEng := permission.NewEngine(permStore, auditor)
	secretMgr := secret.NewManager(vault, secret.NewMemoryBindings(), auditor)
	approvalSvc := approval.New(approval.NewMemoryStore(), approval.NewMemoryTOTP(), vault, jobSvc, permEng, auditor, bus)
	artRoot := getenv("AIE_ARTIFACT_DIR", "")
	artSvc := artifact.New(artifact.NewMemoryStore(), artRoot)
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
	skillSvc := skill.NewService()
	knwSvc := knowledge.NewService()
	bridge := &feishu.Bridge{
		Employees: empSvc, Jobs: jobSvc, Messages: msgSvc,
		Scheduler: sched, Notify: notifySvc, Feishu: feishuSvc,
	}
	bridge.Wire()

	// 开发环境默认注入演示数据，便于 Admin 开箱可看（可用 AIE_SEED_DEMO=0 关闭）
	if getenv("AIE_SEED_DEMO", "1") != "0" {
		seedDemoData(empSvc, wsSvc, wsNodeSvc, presence, jobSvc, feishuSvc, skillSvc, knwSvc)
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
		Auth:         authSvc,
		Enrollment:   enrollSvc,
		CA:           ca,
		Employees:    empSvc,
		Workspaces:   wsSvc,
		Workstations: wsNodeSvc,
		Sessions:     sessSvc,
		Jobs:         jobSvc,
		Messages:     msgSvc,
		Bus:          bus,
		Audit:        auditor,
		Feishu:       feishuSvc,
		Scheduler:    sched,
		Notify:       notifySvc,
		Secrets:         vault,
		SecretMgr:       secretMgr,
		Permission:      permEng,
		PermissionStore: permStore,
		Approvals:       approvalSvc,
		Artifacts:       artSvc,
		Registry:        regSvc,
		Metrics:         met,
		Skills:          skillSvc,
		Knowledge:       knwSvc,
	})
	httpSrv := &http.Server{Addr: cfg.HTTPAddr, Handler: httpHandler}
	go func() {
		fmt.Printf("HTTP 监听 %s\n", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "HTTP 错误: %v\n", err)
		}
	}()

	fmt.Printf("AI Employee Control Plane %s\n", config.Version)
	fmt.Printf("环境=%s gRPC(mTLS)=%s\n", cfg.Env, lis.Addr().String())
	fmt.Printf("默认管理员: %s / (见 AIE_ADMIN_PASSWORD，默认 admin123)\n", adminUser)

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	_ = httpSrv.Close()
	fmt.Println("已关闭")
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

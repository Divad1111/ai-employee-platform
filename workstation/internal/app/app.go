// Package app 组装 CLI / Daemon 顶层命令。
// 设计依据：设计文档 §40–§44、§19；决策 Q-05。
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ai-employee-platform/workstation/internal/config"
	grpcclient "github.com/ai-employee-platform/workstation/internal/controlplane/grpc"
	"github.com/ai-employee-platform/workstation/internal/daemon"
	"github.com/ai-employee-platform/workstation/internal/diagnostics"
	"github.com/ai-employee-platform/workstation/internal/identity"
	"github.com/ai-employee-platform/workstation/internal/ipc"
	"github.com/ai-employee-platform/workstation/internal/platform"
	"github.com/ai-employee-platform/workstation/internal/providers"
	"github.com/ai-employee-platform/workstation/internal/service"
	"github.com/ai-employee-platform/workstation/internal/updater"
)

// Run 根据命令行参数分发子命令。
func Run(args []string) error {
	if len(args) == 0 {
		printHelp()
		return nil
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("aew %s\n", config.Version)
		return nil
	case "help", "--help", "-h":
		printHelp()
		return nil
	case "register":
		return runRegister(args[1:])
	case "unregister":
		return runUnregister(args[1:])
	case "ping":
		return runPing(args[1:])
	case "status":
		return runStatus(args[1:])
	case "doctor":
		return runDoctor(args[1:])
	case "logs":
		return runLogs(args[1:])
	case "reconnect":
		return runReconnect()
	case "daemon":
		return runDaemon(args[1:])
	case "service":
		return runService(args[1:])
	case "employee":
		return runEmployee(args[1:])
	case "workspace":
		return runWorkspace(args[1:])
	case "session":
		return runSession(args[1:])
	case "job":
		return runJob(args[1:])
	case "agent":
		return runAgent(args[1:])
	case "update":
		return runUpdate(args[1:])
	case "config":
		return runConfig(args[1:])
	default:
		return fmt.Errorf("未知命令 %q", args[0])
	}
}

func printHelp() {
	fmt.Println(`aew — AI Employee Workstation

用法:
  aew version
  aew register --server https://host:8080 --token <enrollment_token>
  aew unregister
  aew ping [--grpc host:9090]
  aew status
  aew doctor [--fix] [--dry-run=false]
  aew logs [--tail N]
  aew reconnect
  aew daemon [--skip-connect]
  aew service install|uninstall|start|stop|restart|status
  aew employee ensure --id EMP-1 --name Alice [--provider cursor]
  aew workspace ensure --id WS-1 --employee EMP-1 [--path DIR]
  aew session start|stop --id SES-1 ...
  aew job run --id JOB-1 --employee EMP-1 --session SES-1 --prompt "..."
  aew agent detect|install|update|uninstall --provider cursor
  aew update check|install|rollback
  aew config show
  aew help`)
}

func ipcClient() *ipc.Client {
	return &ipc.Client{Transport: ipc.DefaultTransport()}
}

func ipcCall(method string, params any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return ipcClient().Call(ctx, method, params)
}

func runDaemon(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	skip := fs.Bool("skip-connect", false, "跳过 Control Plane 连接（本地调试）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths := platform.Detect()
	cfgPath := filepath.Join(paths.ConfigDir(), "config.yaml")
	cfg, err := config.LoadFile(cfgPath)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	d := daemon.New(daemon.Options{
		Paths: paths, Config: cfg, SkipConnect: *skip,
	})
	return d.Run(ctx)
}

func runService(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("需要 install|uninstall|start|stop|restart|status")
	}
	mgr := service.New()
	switch args[0] {
	case "install":
		return mgr.Install("")
	case "uninstall":
		return mgr.Uninstall()
	case "start":
		return mgr.Start()
	case "stop":
		return mgr.Stop()
	case "restart":
		return mgr.Restart()
	case "status":
		s, err := mgr.Status()
		fmt.Println(s)
		return err
	default:
		return fmt.Errorf("未知 service 子命令")
	}
}

func runStatus(args []string) error {
	_ = args
	// 优先 IPC；失败则本地降级
	raw, err := ipcCall("status", nil)
	if err != nil {
		return runStatusLocal()
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	fmt.Println("Mode           : daemon")
	printKV(m)
	return nil
}

func runStatusLocal() error {
	paths := platform.Detect()
	b, err := identity.Load(paths)
	if err != nil {
		fmt.Println("Status         : NOT_REGISTERED")
		fmt.Printf("Identity Dir   : %s\n", paths.IdentityDir())
		return nil
	}
	fmt.Println("Mode           : local")
	fmt.Printf("Workstation ID : %s\n", b.WorkstationID)
	fmt.Printf("Identity Dir   : %s\n", paths.IdentityDir())
	fmt.Printf("Version        : %s\n", config.Version)
	return nil
}

func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fix := fs.Bool("fix", false, "仅自动修复安全白名单项")
	dry := fs.Bool("dry-run", true, "fix 时默认 dry-run；加 --dry-run=false 才写入")
	_ = fs.Parse(args)
	paths := platform.Detect()
	r := &diagnostics.Runner{DataDir: paths.DataDir()}
	if *fix {
		fixed, skipped, err := r.Fix(*dry)
		if err != nil {
			return err
		}
		fmt.Println("doctor --fix")
		for _, f := range fixed {
			fmt.Println("  fixed:", f)
		}
		for _, s := range skipped {
			fmt.Println("  skipped:", s)
		}
		if *dry {
			fmt.Println("（默认 dry-run；危险修复已禁止）")
		}
		return nil
	}
	fmt.Print(diagnostics.Format(r.Run()))
	raw, err := ipcCall("doctor", nil)
	if err != nil {
		fmt.Println("Daemon         : OFFLINE")
		if _, e := identity.Load(paths); e != nil {
			fmt.Println("Identity       : MISSING")
		} else {
			fmt.Println("Identity       : OK")
		}
		return nil
	}
	fmt.Println(string(raw))
	return nil
}

func runLogs(args []string) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	tail := fs.Int("tail", 50, "显示末尾行数")
	_ = fs.Parse(args)
	paths := platform.Detect()
	logFile := filepath.Join(paths.LogDir(), "aew.log")
	b, err := os.ReadFile(logFile)
	if err != nil {
		fmt.Printf("无日志文件: %s\n", logFile)
		return nil
	}
	lines := strings.Split(string(b), "\n")
	start := 0
	if len(lines) > *tail {
		start = len(lines) - *tail
	}
	for _, line := range lines[start:] {
		fmt.Println(line)
	}
	return nil
}

func runReconnect() error {
	fmt.Println("请重启 daemon 或等待自动指数退避重连（reconnect 模块）。")
	return nil
}

func runEmployee(args []string) error {
	if len(args) == 0 || args[0] != "ensure" {
		return fmt.Errorf("用法: aew employee ensure --id ID --name NAME")
	}
	fs := flag.NewFlagSet("employee", flag.ContinueOnError)
	id := fs.String("id", "", "")
	name := fs.String("name", "", "")
	prov := fs.String("provider", "cursor", "")
	_ = fs.Parse(args[1:])
	raw, err := ipcCall("employee.ensure", map[string]string{"ID": *id, "Name": *name, "Provider": *prov})
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

func runWorkspace(args []string) error {
	if len(args) == 0 || args[0] != "ensure" {
		return fmt.Errorf("用法: aew workspace ensure --id ID --employee EMP [--path DIR]")
	}
	fs := flag.NewFlagSet("workspace", flag.ContinueOnError)
	id := fs.String("id", "", "")
	emp := fs.String("employee", "", "")
	path := fs.String("path", "", "")
	_ = fs.Parse(args[1:])
	raw, err := ipcCall("workspace.ensure", map[string]string{"ID": *id, "EmployeeID": *emp, "Path": *path})
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

func runSession(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: aew session start|stop ...")
	}
	switch args[0] {
	case "start":
		fs := flag.NewFlagSet("session-start", flag.ContinueOnError)
		id := fs.String("id", "", "")
		emp := fs.String("employee", "", "")
		ws := fs.String("workspace", "", "")
		prov := fs.String("provider", "", "")
		_ = fs.Parse(args[1:])
		raw, err := ipcCall("session.start", map[string]string{
			"ID": *id, "EmployeeID": *emp, "WorkspaceID": *ws, "Provider": *prov,
		})
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	case "stop":
		fs := flag.NewFlagSet("session-stop", flag.ContinueOnError)
		id := fs.String("id", "", "")
		_ = fs.Parse(args[1:])
		raw, err := ipcCall("session.stop", map[string]string{"ID": *id})
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	default:
		return fmt.Errorf("未知 session 子命令")
	}
}

func runJob(args []string) error {
	if len(args) == 0 || args[0] != "run" {
		return fmt.Errorf("用法: aew job run --id ID --employee EMP --session SES --prompt TEXT")
	}
	fs := flag.NewFlagSet("job-run", flag.ContinueOnError)
	id := fs.String("id", "", "")
	emp := fs.String("employee", "", "")
	ses := fs.String("session", "", "")
	prompt := fs.String("prompt", "", "")
	_ = fs.Parse(args[1:])
	raw, err := ipcCall("job.run", map[string]string{
		"ID": *id, "EmployeeID": *emp, "SessionID": *ses, "Prompt": *prompt,
	})
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

func runAgent(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: aew agent detect|install|update|uninstall --provider cursor")
	}
	switch args[0] {
	case "detect":
		fs := flag.NewFlagSet("agent-detect", flag.ContinueOnError)
		name := fs.String("provider", "cursor", "")
		_ = fs.Parse(args[1:])
		raw, err := ipcCall("provider.detect", map[string]string{"Name": *name})
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	case "install", "update":
		fs := flag.NewFlagSet("agent-install", flag.ContinueOnError)
		name := fs.String("provider", "cursor", "")
		file := fs.String("file", "", "本地包路径")
		sha := fs.String("sha256", "", "")
		sig := fs.String("signature", "", "base64 ed25519")
		ver := fs.String("version", "dev", "")
		pubkey := fs.String("pubkey", "", "CP 下发的 ed25519 公钥 base64（Q-06 B）")
		_ = fs.Parse(args[1:])
		if *file == "" {
			return fmt.Errorf("需要 --file")
		}
		content, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		paths := platform.Detect()
		inst, err := providers.NewInstaller(paths.DataDir(), *pubkey)
		if err != nil {
			return err
		}
		if err := inst.Install(*name, *ver, content, *sha, *sig); err != nil {
			return err
		}
		fmt.Printf("provider=%s status=%s event=PROVIDER_INSTALLED\n", *name, inst.Status(*name))
		return nil
	case "uninstall":
		fs := flag.NewFlagSet("agent-uninstall", flag.ContinueOnError)
		name := fs.String("provider", "cursor", "")
		_ = fs.Parse(args[1:])
		paths := platform.Detect()
		inst, err := providers.NewInstaller(paths.DataDir(), "")
		if err != nil {
			return err
		}
		return inst.Uninstall(*name)
	default:
		return fmt.Errorf("未知 agent 子命令 %q", args[0])
	}
}

func runUpdate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: aew update check|install|rollback")
	}
	paths := platform.Detect()
	mgr := updater.New(filepath.Join(paths.DataDir(), "update"), nil, nil)
	switch args[0] {
	case "check":
		fs := flag.NewFlagSet("update-check", flag.ContinueOnError)
		remote := fs.String("version", "", "")
		_ = fs.Parse(args[1:])
		need, cur := mgr.Check(*remote)
		fmt.Printf("current=%s need_update=%v\n", cur, need)
		return nil
	case "install":
		fs := flag.NewFlagSet("update-install", flag.ContinueOnError)
		file := fs.String("file", "", "")
		ver := fs.String("version", "", "")
		sha := fs.String("sha256", "", "")
		_ = fs.Parse(args[1:])
		if *file == "" || *ver == "" {
			return fmt.Errorf("需要 --file --version")
		}
		b, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		mgr.BeginDrain()
		if err := mgr.Install(*ver, b, *sha, ""); err != nil {
			return err
		}
		fmt.Printf("updated to %s\n", mgr.Snapshot().Current)
		return nil
	case "rollback":
		if err := mgr.Rollback(); err != nil {
			return err
		}
		fmt.Printf("rolled back to %s\n", mgr.Snapshot().Current)
		return nil
	default:
		return fmt.Errorf("未知 update 子命令")
	}
}

func runConfig(args []string) error {
	_ = args
	paths := platform.Detect()
	cfg, err := config.LoadFile(filepath.Join(paths.ConfigDir(), "config.yaml"))
	if err != nil {
		return err
	}
	fmt.Printf("name=%s endpoint=%s max_sessions=%d\n", cfg.Name, cfg.ControlPlaneEndpoint, cfg.MaxSessions)
	fmt.Println("（敏感信息不在 YAML 中）")
	return nil
}

func printKV(m map[string]any) {
	for k, v := range m {
		fmt.Printf("%-15s: %v\n", k, v)
	}
}

func runRegister(args []string) error {
	fs := flag.NewFlagSet("register", flag.ContinueOnError)
	server := fs.String("server", "http://127.0.0.1:8080", "Control Plane HTTP 地址")
	token := fs.String("token", "", "Enrollment Token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *token == "" {
		return fmt.Errorf("需要 --token")
	}
	paths := platform.Detect()
	b, _, err := identity.Generate()
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{
		"token": *token, "workstation_id": b.WorkstationID, "csr_pem": string(b.CSRPEM),
	})
	url := strings.TrimRight(*server, "/") + "/api/enrollment/enroll"
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("enroll 失败: %s %s", resp.Status, string(raw))
	}
	var out struct {
		CAPEM       string `json:"ca_pem"`
		Certificate string `json:"certificate"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	b.CAPEM = []byte(out.CAPEM)
	b.CertPEM = []byte(out.Certificate)
	if err := identity.Save(paths, b); err != nil {
		return err
	}
	fmt.Printf("注册成功\nWorkstation ID : %s\nIdentity Dir   : %s\n", b.WorkstationID, paths.IdentityDir())
	fmt.Println("私钥仅保存在本地，未上传 Control Plane。")
	return nil
}

func runUnregister(args []string) error {
	_ = args
	paths := platform.Detect()
	b, err := identity.Load(paths)
	if err != nil {
		return err
	}
	if err := identity.Clear(paths); err != nil {
		return err
	}
	fmt.Printf("已清理本地身份（原 ID: %s）。\n", b.WorkstationID)
	return nil
}

func runPing(args []string) error {
	fs := flag.NewFlagSet("ping", flag.ContinueOnError)
	grpcAddr := fs.String("grpc", "127.0.0.1:9090", "Control Plane gRPC 地址")
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths := platform.Detect()
	b, err := identity.Load(paths)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cli, err := grpcclient.Dial(ctx, *grpcAddr, b)
	if err != nil {
		return err
	}
	defer cli.Close()
	resp, err := cli.Ping(ctx, "aew-ping")
	if err != nil {
		return err
	}
	fmt.Println("Control Plane : OK")
	fmt.Println("TLS           : OK")
	fmt.Println("mTLS          : OK")
	fmt.Printf("Server Time   : %d\n", resp.ServerUnixMs)
	fmt.Printf("Sequence      : %d\n", resp.CurrentCommandSequence)
	fmt.Printf("Nonce         : %s\n", resp.Nonce)
	return nil
}

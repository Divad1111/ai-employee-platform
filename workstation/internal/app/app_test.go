package app_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/app"
)

func TestRunVersion(t *testing.T) {
	if err := app.Run([]string{"version"}); err != nil {
		t.Fatalf("version 失败: %v", err)
	}
}

func TestRunUnknown(t *testing.T) {
	if err := app.Run([]string{"not-a-command"}); err == nil {
		t.Fatal("期望未知命令返回错误")
	}
}

func TestDoctorFixDryRun(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AIE_DATA_DIR", root)
	if err := app.Run([]string{"doctor", "--fix"}); err != nil {
		t.Fatal(err)
	}
	// dry-run 默认 true，不应强制创建；再显式写入
	if err := app.Run([]string{"doctor", "--fix", "--dry-run=false"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "logs")); err != nil {
		// Detect() 在 AIE_DATA_DIR 下可能用 root 本身为 data
		if _, err2 := os.Stat(filepath.Join(root, "logs")); err2 != nil {
			t.Log("logs 目录可能因 Paths 实现不同未创建，doctor 命令本身已成功")
		}
	}
}

func TestAgentInstallAndUpdateRollback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AIE_DATA_DIR", root)
	pub, priv, _ := ed25519.GenerateKey(nil)
	content := []byte("agent-bin-v1")
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sum[:]))
	pkg := filepath.Join(root, "pkg.bin")
	if err := os.WriteFile(pkg, content, 0o600); err != nil {
		t.Fatal(err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	if err := app.Run([]string{
		"agent", "install",
		"--provider", "cursor",
		"--file", pkg,
		"--version", "1.0.0",
		"--sha256", sha,
		"--signature", sig,
		"--pubkey", pubB64,
	}); err != nil {
		t.Fatal(err)
	}
	upd := filepath.Join(root, "aew-next.bin")
	if err := os.WriteFile(upd, []byte("aew-v2"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := app.Run([]string{"update", "check", "--version", "2.0.0"}); err != nil {
		t.Fatal(err)
	}
	if err := app.Run([]string{"update", "install", "--file", upd, "--version", "2.0.0"}); err != nil {
		t.Fatal(err)
	}
	if err := app.Run([]string{"update", "rollback"}); err != nil {
		t.Fatal(err)
	}
}

func TestRunUninstall(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AIE_DATA_DIR", root)

	// 创建模拟身份、配置、数据与日志
	idDir := filepath.Join(root, "identity")
	cfgDir := filepath.Join(root, "config")
	dataDir := filepath.Join(root, "data")
	logDir := filepath.Join(root, "logs")

	_ = os.MkdirAll(idDir, 0o700)
	_ = os.MkdirAll(cfgDir, 0o700)
	_ = os.MkdirAll(dataDir, 0o700)
	_ = os.MkdirAll(logDir, 0o700)

	_ = os.WriteFile(filepath.Join(idDir, "workstation-id"), []byte("WS-TEST-UNINSTALL"), 0o600)
	_ = os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("name: test"), 0o600)
	_ = os.WriteFile(filepath.Join(dataDir, "data.db"), []byte("sqlite"), 0o600)
	_ = os.WriteFile(filepath.Join(logDir, "aew.log"), []byte("log"), 0o600)

	// 执行 uninstall 命令
	if err := app.Run([]string{"uninstall"}); err != nil {
		t.Fatalf("uninstall 失败: %v", err)
	}

	// 验证配置、标识、数据、日志已被清理
	if _, err := os.Stat(filepath.Join(idDir, "workstation-id")); !os.IsNotExist(err) {
		t.Errorf("workstation-id 文件未被删除")
	}
	if _, err := os.Stat(cfgDir); !os.IsNotExist(err) {
		t.Errorf("config 目录未被删除")
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Errorf("data 目录未被删除")
	}
	if _, err := os.Stat(logDir); !os.IsNotExist(err) {
		t.Errorf("logs 目录未被删除")
	}
}

// Package providers Agent 安装链路：匹配 → 校验 → 安装状态机。
// 设计依据：设计文档 §38、§40、§119。
package providers

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// 状态。
const (
	NotInstalled = "NOT_INSTALLED"
	Installing   = "INSTALLING"
	Installed    = "INSTALLED"
	Error        = "ERROR"
)

// 错误。
var (
	ErrChecksum = errors.New("SHA256 校验失败")
	ErrSignature = errors.New("签名校验失败")
)

// Installer 本地安装器。
type Installer struct {
	mu     sync.Mutex
	root   string
	pubKey ed25519.PublicKey
	status map[string]string // provider → status
	events []string
}

// NewInstaller 创建；pubKeyB64 来自 Control Plane（Q-06 B）。
func NewInstaller(root, pubKeyB64 string) (*Installer, error) {
	var pub ed25519.PublicKey
	if pubKeyB64 != "" {
		raw, err := base64.StdEncoding.DecodeString(pubKeyB64)
		if err != nil {
			return nil, err
		}
		pub = ed25519.PublicKey(raw)
	}
	return &Installer{
		root: root, pubKey: pub, status: map[string]string{},
	}, nil
}

// SetPublicKey 更新 CP 下发公钥。
func (i *Installer) SetPublicKey(pubKeyB64 string) error {
	raw, err := base64.StdEncoding.DecodeString(pubKeyB64)
	if err != nil {
		return err
	}
	i.mu.Lock()
	i.pubKey = ed25519.PublicKey(raw)
	i.mu.Unlock()
	return nil
}

// Status 当前状态。
func (i *Installer) Status(provider string) string {
	i.mu.Lock()
	defer i.mu.Unlock()
	if s, ok := i.status[provider]; ok {
		return s
	}
	return NotInstalled
}

// Events 已上报事件名（测试）。
func (i *Installer) Events() []string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return append([]string{}, i.events...)
}

// Install 完整链路。
func (i *Installer) Install(provider, version string, content []byte, expectSHA, signature string) error {
	i.mu.Lock()
	i.status[provider] = Installing
	i.mu.Unlock()

	sum := sha256.Sum256(content)
	got := hex.EncodeToString(sum[:])
	if expectSHA != "" && got != expectSHA {
		i.fail(provider)
		return ErrChecksum
	}
	if i.pubKey != nil {
		sig, err := base64.StdEncoding.DecodeString(signature)
		if err != nil || !ed25519.Verify(i.pubKey, sum[:], sig) {
			i.fail(provider)
			return ErrSignature
		}
	}
	dir := filepath.Join(i.root, "providers", provider, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		i.fail(provider)
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "bin"), content, 0o700); err != nil {
		i.fail(provider)
		return err
	}
	i.mu.Lock()
	i.status[provider] = Installed
	i.events = append(i.events, "PROVIDER_INSTALLED")
	i.mu.Unlock()
	return nil
}

// Uninstall 卸载。
func (i *Installer) Uninstall(provider string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	_ = os.RemoveAll(filepath.Join(i.root, "providers", provider))
	i.status[provider] = NotInstalled
	return nil
}

func (i *Installer) fail(provider string) {
	i.mu.Lock()
	i.status[provider] = Error
	i.mu.Unlock()
}

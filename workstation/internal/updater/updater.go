// Package updater Workstation 自更新与回滚（保留 N / N-1）。
// 设计依据：设计文档 §70、§97、§93。
package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// 错误。
var (
	ErrHealthFailed = errors.New("health check 失败，已回滚")
	ErrBadPackage   = errors.New("更新包校验失败")
)

// Verifier 包校验（SHA256 + 签名）。
type Verifier interface {
	Verify(content []byte, sha256Hex, signature string) error
}

// State 更新状态。
type State struct {
	Current string `json:"current"`
	Previous string `json:"previous"`
	Draining bool  `json:"draining"`
}

// Manager 本地版本目录：versions/N 与 versions/N-1。
type Manager struct {
	mu       sync.Mutex
	root     string
	verifier Verifier
	health   func() error
	state    State
}

// New 创建；从磁盘恢复 CURRENT / N-1（跨进程 CLI 可用）。
func New(root string, v Verifier, health func() error) *Manager {
	if health == nil {
		health = func() error { return nil }
	}
	m := &Manager{root: root, verifier: v, health: health, state: State{Current: "N"}}
	m.load()
	return m
}

func (m *Manager) load() {
	dir := filepath.Join(m.root, "versions")
	if b, err := os.ReadFile(filepath.Join(dir, "CURRENT")); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			m.state.Current = s
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "N-1")); err == nil {
		m.state.Previous = strings.TrimSpace(string(b))
	}
}

func (m *Manager) persist() {
	dir := filepath.Join(m.root, "versions")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "CURRENT"), []byte(m.state.Current), 0o600)
	if m.state.Previous != "" {
		_ = os.WriteFile(filepath.Join(dir, "N-1"), []byte(m.state.Previous), 0o600)
	}
}

// BeginDrain 更新前进入 DRAINING。
func (m *Manager) BeginDrain() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Draining = true
}

// Check 检查是否有可用更新（元数据）。
func (m *Manager) Check(remoteVersion string) (need bool, current string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return remoteVersion != "" && remoteVersion != m.state.Current, m.state.Current
}

// Install 校验并切换；Health Check 失败回滚 N-1。
func (m *Manager) Install(version string, content []byte, sha256Hex, signature string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Draining = true
	if m.verifier != nil {
		if err := m.verifier.Verify(content, sha256Hex, signature); err != nil {
			return ErrBadPackage
		}
	} else {
		sum := sha256.Sum256(content)
		if sha256Hex != "" && hex.EncodeToString(sum[:]) != sha256Hex {
			return ErrBadPackage
		}
	}
	dir := filepath.Join(m.root, "versions")
	_ = os.MkdirAll(dir, 0o755)
	prev := m.state.Current
	nextPath := filepath.Join(dir, version)
	if err := os.WriteFile(nextPath, content, 0o700); err != nil {
		return err
	}
	m.state.Previous = prev
	m.state.Current = version
	if err := m.health(); err != nil {
		// 回滚
		m.state.Current = m.state.Previous
		m.state.Draining = false
		m.persist()
		return ErrHealthFailed
	}
	m.state.Draining = false
	m.persist()
	return nil
}

// Rollback 手动回滚。
func (m *Manager) Rollback() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Previous == "" {
		return errors.New("无 N-1 可回滚")
	}
	m.state.Current, m.state.Previous = m.state.Previous, m.state.Current
	m.persist()
	return nil
}

// Snapshot 状态。
func (m *Manager) Snapshot() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

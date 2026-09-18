// Package security 本地策略门禁：Process/Git/Filesystem 调用前必须过 Gate。
// 无「绕过 Engine 的任意 shell」路径；DENY 阻断，ASK 上报等待审批。
// 设计依据：设计文档 §59、§60、§116。
package security

import (
	"errors"
	"sync"
)

// 与 Control Plane 对齐的决策常量。
const (
	EffectAllow = "ALLOW"
	EffectAsk   = "ASK"
	EffectDeny  = "DENY"
)

// 错误。
var (
	ErrDenied     = errors.New("策略 DENY：操作被阻断")
	ErrNeedAsk    = errors.New("策略 ASK：需要审批")
	ErrUnknownAct = errors.New("未知动作，默认 DENY")
)

// Rule 缓存规则。
type Rule struct {
	Action   string `json:"action"`
	Effect   string `json:"effect"`
	Critical bool   `json:"critical"`
}

// Gate 本地策略门。
type Gate struct {
	mu    sync.RWMutex
	rules map[string]Rule
}

// NewGate 创建；默认空规则 → 一律 DENY。
func NewGate() *Gate {
	return &Gate{rules: map[string]Rule{}}
}

// Sync 从 Control Plane 同步规则缓存。
func (g *Gate) Sync(rules []Rule) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rules = map[string]Rule{}
	for _, r := range rules {
		g.rules[r.Action] = r
	}
}

// Check 执行前校验。
func (g *Gate) Check(action string) (effect string, err error) {
	g.mu.RLock()
	r, ok := g.rules[action]
	g.mu.RUnlock()
	if !ok {
		return EffectDeny, ErrUnknownAct
	}
	switch r.Effect {
	case EffectAllow:
		return EffectAllow, nil
	case EffectAsk:
		return EffectAsk, ErrNeedAsk
	default:
		return EffectDeny, ErrDenied
	}
}

// MustAllow 仅 ALLOW 可通过；DENY/ASK/未知均失败（CLI 旁路防护）。
func (g *Gate) MustAllow(action string) error {
	_, err := g.Check(action)
	return err
}

// HasShellBypass 恒为 false：架构上不提供任意 shell 旁路。
func HasShellBypass() bool { return false }

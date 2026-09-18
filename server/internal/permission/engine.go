// Package permission 实现 Permission Engine（ALLOW / ASK / DENY）。
// 未配置规则默认 DENY，禁止「未配置即放行」。
// 设计依据：设计文档 §28、§29、§88。
package permission

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/idgen"
)

// 决策结果。
const (
	EffectAllow = "ALLOW"
	EffectAsk   = "ASK"
	EffectDeny  = "DENY"
)

// 标准动作（§29 + CRITICAL 示例）。
const (
	ActionWorkspaceRead   = "workspace.read"
	ActionWorkspaceWrite  = "workspace.write"
	ActionGitStatus       = "git.status"
	ActionGitCommit       = "git.commit"
	ActionGitPush         = "git.push"
	ActionFSDeleteBulk    = "fs.delete_bulk"
	ActionShellPowerShell = "shell.powershell"
	ActionFSSystemModify  = "fs.system_modify"
	ActionSystemShutdown  = "system.shutdown"
	ActionDeployProd      = "deploy.production"
	ActionWorkspaceDelete = "workspace.delete"
	ActionCredentialRot   = "credential.rotate"
)

// 错误。
var (
	ErrProfileNotFound = errors.New("permission profile 不存在")
	ErrInvalidEffect   = errors.New("无效 effect")
)

// Rule 单条规则。
type Rule struct {
	ID          string `json:"id"`
	ProfileID   string `json:"profile_id"`
	Action      string `json:"action"`
	Effect      string `json:"effect"`
	Critical    bool   `json:"critical"`
	Priority    int    `json:"priority"`
	Description string `json:"description"`
}

// Profile 权限配置。
type Profile struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsDefault   bool      `json:"is_default"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Request 决策输入。
type Request struct {
	ActorType   string            `json:"actor_type"` // EMPLOYEE / USER / SYSTEM
	ActorID     string            `json:"actor_id"`
	Action      string            `json:"action"`
	TargetType  string            `json:"target_type"`
	TargetID    string            `json:"target_id"`
	ProfileID   string            `json:"profile_id"`
	JobID       string            `json:"job_id"`
	Context     map[string]string `json:"context"`
}

// Decision 决策输出。
type Decision struct {
	Effect   string `json:"effect"`
	Reason   string `json:"reason"`
	Critical bool   `json:"critical"`
	RuleID   string `json:"rule_id,omitempty"`
	Action   string `json:"action"`
	Profile  string `json:"profile_id"`
}

// Auditor 审计。
type Auditor interface {
	Log(ctx context.Context, actorType, actorID, action, result, ip string, meta map[string]string)
}

// Store 配置存储。
type Store interface {
	SaveProfile(ctx context.Context, p *Profile) error
	GetProfile(ctx context.Context, id string) (*Profile, error)
	DefaultProfileID(ctx context.Context) string
	ListProfiles(ctx context.Context) ([]*Profile, error)
	UpsertRule(ctx context.Context, r *Rule) error
	ListRules(ctx context.Context, profileID string) ([]*Rule, error)
	GetRule(ctx context.Context, profileID, action string) (*Rule, error)
}

// Engine 决策引擎。
type Engine struct {
	store       Store
	audit       Auditor
	auditAllow  bool // true=全量审计 ALLOW；false=仅 ASK/DENY
	mu          sync.RWMutex
}

// NewEngine 创建引擎。
func NewEngine(store Store, audit Auditor) *Engine {
	return &Engine{store: store, audit: audit, auditAllow: false}
}

// SetAuditAllow 配置是否审计 ALLOW。
func (e *Engine) SetAuditAllow(v bool) { e.auditAllow = v }

// Decide 根据 Profile 规则决策；无匹配规则 → DENY。
func (e *Engine) Decide(ctx context.Context, req Request) Decision {
	profileID := req.ProfileID
	if profileID == "" {
		profileID = e.store.DefaultProfileID(ctx)
	}
	d := Decision{Action: req.Action, Profile: profileID, Effect: EffectDeny, Reason: "未配置规则，默认 DENY"}

	rule, err := e.store.GetRule(ctx, profileID, req.Action)
	if err == nil && rule != nil {
		d.Effect = rule.Effect
		d.Critical = rule.Critical
		d.RuleID = rule.ID
		d.Reason = rule.Description
		if d.Reason == "" {
			d.Reason = fmt.Sprintf("匹配规则 %s → %s", rule.Action, rule.Effect)
		}
	}

	shouldAudit := d.Effect != EffectAllow || e.auditAllow
	if shouldAudit && e.audit != nil {
		e.audit.Log(ctx, req.ActorType, req.ActorID, "permission.decide", d.Effect, "", map[string]string{
			"action": req.Action, "target": req.TargetID, "job_id": req.JobID,
			"profile": profileID, "reason": d.Reason, "critical": fmt.Sprintf("%v", d.Critical),
		})
	}
	return d
}

// ExportRules 导出规则（Workstation 缓存同步）。
func (e *Engine) ExportRules(ctx context.Context, profileID string) ([]*Rule, error) {
	if profileID == "" {
		profileID = e.store.DefaultProfileID(ctx)
	}
	rules, err := e.store.ListRules(ctx, profileID)
	if err != nil {
		return nil, err
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority < rules[j].Priority
		}
		return rules[i].Action < rules[j].Action
	})
	return rules, nil
}

// EnsureDefault 写入 §29 默认 Profile（内存/启动）。
func EnsureDefault(store Store) error {
	ctx := context.Background()
	now := time.Now().UTC()
	p := &Profile{
		ID: "default", Name: "Default", Description: "V2 默认权限配置",
		IsDefault: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.SaveProfile(ctx, p); err != nil {
		return err
	}
	defaults := []struct {
		Action, Effect, Desc string
		Critical             bool
	}{
		{ActionWorkspaceRead, EffectAllow, "Workspace 读取", false},
		{ActionWorkspaceWrite, EffectAllow, "Workspace 修改", false},
		{ActionGitStatus, EffectAllow, "Git status", false},
		{ActionGitCommit, EffectAllow, "Git commit", false},
		{ActionGitPush, EffectAsk, "Git push", true},
		{ActionFSDeleteBulk, EffectAsk, "删除大量文件", false},
		{ActionShellPowerShell, EffectAsk, "PowerShell", false},
		{ActionFSSystemModify, EffectDeny, "系统文件修改", true},
		{ActionSystemShutdown, EffectDeny, "shutdown", true},
		{ActionDeployProd, EffectAsk, "Production Deploy", true},
		{ActionWorkspaceDelete, EffectAsk, "Delete Workspace", true},
		{ActionCredentialRot, EffectAsk, "Rotate Credential", true},
	}
	for _, d := range defaults {
		r := &Rule{
			ID: idgen.Raw(), ProfileID: "default", Action: d.Action,
			Effect: d.Effect, Critical: d.Critical, Priority: 10, Description: d.Desc,
		}
		if err := store.UpsertRule(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

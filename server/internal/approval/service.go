// Package approval 审批中心与 CRITICAL TOTP。
// ASK → Approval → Approve/Reject；CRITICAL 未配置 TOTP → DENY。
// 设计依据：设计文档 §28、§30、§113。
package approval

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/idgen"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/permission"
	"github.com/ai-employee-platform/server/internal/secret"
	"github.com/ai-employee-platform/server/internal/totp"
)

// 状态。
const (
	StatusPending  = "PENDING"
	StatusApproved = "APPROVED"
	StatusRejected = "REJECTED"
	StatusDenied   = "DENIED" // CRITICAL 无 TOTP 等直接拒绝
)

// 错误。
var (
	ErrNotFound       = errors.New("approval 不存在")
	ErrInvalidState   = errors.New("审批状态非法")
	ErrTOTPRequired   = errors.New("CRITICAL 操作需要 TOTP")
	ErrTOTPNotConfigured = errors.New("未配置 TOTP，CRITICAL 操作 DENY")
	ErrTOTPInvalid    = errors.New("TOTP 校验失败")
	ErrTOTPLocked     = errors.New("TOTP 尝试过多，已锁定")
)

// Request 审批请求。
type Request struct {
	ID          string            `json:"id"`
	JobID       string            `json:"job_id"`
	EmployeeID  string            `json:"employee_id"`
	Action      string            `json:"action"`
	Effect      string            `json:"effect"`
	Critical    bool              `json:"critical"`
	Reason      string            `json:"reason"`
	Status      string            `json:"status"`
	RequesterID string            `json:"requester_id"`
	ResolvedBy  string            `json:"resolved_by,omitempty"`
	Payload     map[string]string `json:"payload,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	ResolvedAt  time.Time         `json:"resolved_at,omitempty"`
}

// TOTPBinding 用户 TOTP 绑定（Secret 仅存引用）。
type TOTPBinding struct {
	UserID      string
	SecretRef   string
	Enabled     bool
	FailedCount int
	LockedUntil time.Time
}

// Store 审批持久化。
type Store interface {
	Save(ctx context.Context, r *Request) error
	Get(ctx context.Context, id string) (*Request, error)
	List(ctx context.Context) ([]*Request, error)
}

// TOTPStore 绑定存储。
type TOTPStore interface {
	Get(ctx context.Context, userID string) (*TOTPBinding, error)
	Save(ctx context.Context, b *TOTPBinding) error
}

// Auditor / Bus。
type Auditor interface {
	Log(ctx context.Context, actorType, actorID, action, result, ip string, meta map[string]string)
}

type Bus interface {
	Publish(ctx context.Context, typ string, payload map[string]string) eventbus.Event
}

// JobBridge Job 状态与 Timeline。
type JobBridge interface {
	Transition(ctx context.Context, id, to, actorID, ip string, meta map[string]string) (*job.Job, error)
	AppendEvent(ctx context.Context, jobID, eventType string, payload map[string]string) error
	Get(ctx context.Context, id string) (*job.Job, error)
}

// Service 审批服务。
type Service struct {
	store   Store
	totp    TOTPStore
	vault   secret.Store
	jobs    JobBridge
	engine  *permission.Engine
	audit   Auditor
	bus     Bus
	maxFail int
	lockFor time.Duration
	criticalActions map[string]bool
}

// New 创建。
func New(store Store, totpStore TOTPStore, vault secret.Store, jobs JobBridge, eng *permission.Engine, audit Auditor, bus Bus) *Service {
	return &Service{
		store: store, totp: totpStore, vault: vault, jobs: jobs, engine: eng, audit: audit, bus: bus,
		maxFail: 5, lockFor: 15 * time.Minute,
		criticalActions: map[string]bool{
			permission.ActionGitPush:         true,
			permission.ActionDeployProd:      true,
			permission.ActionWorkspaceDelete: true,
			permission.ActionCredentialRot:   true,
		},
	}
}

// IsCritical 是否 CRITICAL 动作。
func (s *Service) IsCritical(action string) bool {
	return s.criticalActions[action]
}

// EvaluateAndMaybeCreate 对敏感操作决策；ASK 则建 Approval 并将 Job → WAITING_APPROVAL。
// CRITICAL 且审批人未绑 TOTP 时直接 DENY（不可自动放行）。
func (s *Service) EvaluateAndMaybeCreate(ctx context.Context, req permission.Request, approverUserID string) (permission.Decision, *Request, error) {
	d := s.engine.Decide(ctx, req)
	if d.Effect == permission.EffectAllow {
		return d, nil, nil
	}
	if d.Effect == permission.EffectDeny {
		return d, nil, nil
	}
	// ASK
	critical := d.Critical || s.IsCritical(req.Action)
	if critical {
		b, _ := s.totp.Get(ctx, approverUserID)
		if b == nil || !b.Enabled || b.SecretRef == "" {
			d.Effect = permission.EffectDeny
			d.Reason = "CRITICAL 未配置 TOTP，强制 DENY"
			if s.audit != nil {
				s.audit.Log(ctx, req.ActorType, req.ActorID, "approval.critical_deny", "denied", "", map[string]string{
					"action": req.Action, "reason": d.Reason,
				})
			}
			ar := &Request{
				ID: idgen.New("APR"), JobID: req.JobID, EmployeeID: req.ActorID,
				Action: req.Action, Effect: permission.EffectDeny, Critical: true,
				Reason: d.Reason, Status: StatusDenied, RequesterID: req.ActorID,
				CreatedAt: time.Now().UTC(), ResolvedAt: time.Now().UTC(),
			}
			_ = s.store.Save(ctx, ar)
			return d, ar, ErrTOTPNotConfigured
		}
	}

	ar := &Request{
		ID: idgen.New("APR"), JobID: req.JobID, EmployeeID: req.ActorID,
		Action: req.Action, Effect: permission.EffectAsk, Critical: critical,
		Reason: d.Reason, Status: StatusPending, RequesterID: req.ActorID,
		Payload: req.Context, CreatedAt: time.Now().UTC(),
	}
	if err := s.store.Save(ctx, ar); err != nil {
		return d, nil, err
	}
	if req.JobID != "" && s.jobs != nil {
		_, _ = s.jobs.Transition(ctx, req.JobID, job.StatusWaitingApproval, "permission", "", map[string]string{
			"approval_id": ar.ID, "action": req.Action,
		})
		_ = s.jobs.AppendEvent(ctx, req.JobID, "APPROVAL_REQUIRED", map[string]string{
			"approval_id": ar.ID, "action": req.Action, "critical": boolStr(critical),
		})
	}
	if s.bus != nil {
		s.bus.Publish(ctx, eventbus.TypeSystemAlert, map[string]string{
			"kind": "approval.request", "approval_id": ar.ID, "job_id": req.JobID, "action": req.Action,
		})
	}
	if s.audit != nil {
		s.audit.Log(ctx, "SYSTEM", req.ActorID, "approval.create", "pending", "", map[string]string{
			"approval_id": ar.ID, "action": req.Action,
		})
	}
	return d, ar, nil
}

// Approve 批准；CRITICAL 必须带有效 TOTP。
func (s *Service) Approve(ctx context.Context, id, userID, totpCode, ip string) (*Request, error) {
	ar, err := s.store.Get(ctx, id)
	if err != nil || ar == nil {
		return nil, ErrNotFound
	}
	if ar.Status != StatusPending {
		return nil, ErrInvalidState
	}
	if ar.Critical {
		if err := s.verifyTOTP(ctx, userID, totpCode, ip); err != nil {
			return nil, err
		}
	}
	ar.Status = StatusApproved
	ar.ResolvedBy = userID
	ar.ResolvedAt = time.Now().UTC()
	_ = s.store.Save(ctx, ar)
	if ar.JobID != "" && s.jobs != nil {
		_, _ = s.jobs.Transition(ctx, ar.JobID, job.StatusRunning, userID, ip, map[string]string{
			"approval_id": ar.ID,
		})
		_ = s.jobs.AppendEvent(ctx, ar.JobID, "APPROVAL_GRANTED", map[string]string{
			"approval_id": ar.ID, "by": userID,
		})
	}
	if s.audit != nil {
		s.audit.Log(ctx, "USER", userID, "approval.approve", "success", ip, map[string]string{"approval_id": id})
	}
	if s.bus != nil {
		s.bus.Publish(ctx, eventbus.TypeJobStatus, map[string]string{
			"job_id": ar.JobID, "status": job.StatusRunning, "approval_id": ar.ID,
		})
	}
	return ar, nil
}

// Reject 拒绝。
func (s *Service) Reject(ctx context.Context, id, userID, ip string) (*Request, error) {
	ar, err := s.store.Get(ctx, id)
	if err != nil || ar == nil {
		return nil, ErrNotFound
	}
	if ar.Status != StatusPending {
		return nil, ErrInvalidState
	}
	ar.Status = StatusRejected
	ar.ResolvedBy = userID
	ar.ResolvedAt = time.Now().UTC()
	_ = s.store.Save(ctx, ar)
	if ar.JobID != "" && s.jobs != nil {
		_, _ = s.jobs.Transition(ctx, ar.JobID, job.StatusFailed, userID, ip, map[string]string{
			"approval_id": ar.ID, "result": "rejected",
		})
		_ = s.jobs.AppendEvent(ctx, ar.JobID, "APPROVAL_REJECTED", map[string]string{
			"approval_id": ar.ID, "by": userID,
		})
	}
	if s.audit != nil {
		s.audit.Log(ctx, "USER", userID, "approval.reject", "success", ip, map[string]string{"approval_id": id})
	}
	return ar, nil
}

func (s *Service) verifyTOTP(ctx context.Context, userID, code, ip string) error {
	b, err := s.totp.Get(ctx, userID)
	if err != nil || b == nil || !b.Enabled {
		if s.audit != nil {
			s.audit.Log(ctx, "USER", userID, "approval.totp", "not_configured", ip, nil)
		}
		return ErrTOTPNotConfigured
	}
	if !b.LockedUntil.IsZero() && time.Now().Before(b.LockedUntil) {
		return ErrTOTPLocked
	}
	if code == "" {
		return ErrTOTPRequired
	}
	plain, err := s.vault.Get(b.SecretRef)
	if err != nil || plain == "" {
		return ErrTOTPNotConfigured
	}
	if !totp.Verify(plain, code, time.Now()) {
		b.FailedCount++
		if b.FailedCount >= s.maxFail {
			b.LockedUntil = time.Now().Add(s.lockFor)
			b.FailedCount = 0
		}
		_ = s.totp.Save(ctx, b)
		if s.audit != nil {
			s.audit.Log(ctx, "USER", userID, "approval.totp", "failed", ip, nil)
		}
		return ErrTOTPInvalid
	}
	b.FailedCount = 0
	b.LockedUntil = time.Time{}
	_ = s.totp.Save(ctx, b)
	return nil
}

// EnrollTOTP 绑定 TOTP；返回一次性明文 secret（仅此响应，随后仅存引用）。
func (s *Service) EnrollTOTP(ctx context.Context, userID string) (secretPlain string, binding *TOTPBinding, err error) {
	return s.EnrollTOTPWithIP(ctx, userID, "")
}

// EnrollTOTPWithIP 带客户端 IP 记录绑定 TOTP。
func (s *Service) EnrollTOTPWithIP(ctx context.Context, userID, ip string) (secretPlain string, binding *TOTPBinding, err error) {
	sec, err := totp.GenerateSecret()
	if err != nil {
		return "", nil, err
	}
	var secretRef string
	if encrypter, ok := s.vault.(interface{ Encrypt(string) (string, error) }); ok {
		encrypted, err := encrypter.Encrypt(sec)
		if err != nil {
			return "", nil, err
		}
		secretRef = encrypted
	} else {
		ref, err := s.vault.Put("totp."+userID, sec)
		if err != nil {
			return "", nil, err
		}
		secretRef = ref.ID
	}
	b := &TOTPBinding{UserID: userID, SecretRef: secretRef, Enabled: true}
	if err := s.totp.Save(ctx, b); err != nil {
		return "", nil, err
	}
	if s.audit != nil {
		s.audit.Log(ctx, "USER", userID, "totp.enroll", "success", ip, map[string]string{"ref": secretRef})
	}
	return sec, b, nil
}

// EnrollPendingTOTP 生成 TOTP 秘钥并记录待激活绑定（Enabled: false，等待用户扫描输入动态码确认激活）。
func (s *Service) EnrollPendingTOTP(ctx context.Context, userID, ip string) (secretPlain string, binding *TOTPBinding, err error) {
	sec, err := totp.GenerateSecret()
	if err != nil {
		return "", nil, err
	}
	var secretRef string
	if encrypter, ok := s.vault.(interface{ Encrypt(string) (string, error) }); ok {
		encrypted, err := encrypter.Encrypt(sec)
		if err != nil {
			return "", nil, err
		}
		secretRef = encrypted
	} else {
		ref, err := s.vault.Put("totp."+userID, sec)
		if err != nil {
			return "", nil, err
		}
		secretRef = ref.ID
	}
	b := &TOTPBinding{UserID: userID, SecretRef: secretRef, Enabled: false}
	if err := s.totp.Save(ctx, b); err != nil {
		return "", nil, err
	}
	if s.audit != nil {
		s.audit.Log(ctx, "USER", userID, "totp.enroll_pending", "success", ip, map[string]string{"ref": secretRef})
	}
	return sec, b, nil
}

// ConfirmTOTP 校验用户输入的 6 位动态码，若成功则将 TOTPBinding.Enabled 设为 true 并落盘持久化。
func (s *Service) ConfirmTOTP(ctx context.Context, userID, code, ip string) error {
	b, err := s.totp.Get(ctx, userID)
	if err != nil || b == nil || b.SecretRef == "" {
		return ErrTOTPNotConfigured
	}
	if !b.LockedUntil.IsZero() && time.Now().Before(b.LockedUntil) {
		return ErrTOTPLocked
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return ErrTOTPRequired
	}
	plain, err := s.vault.Get(b.SecretRef)
	if err != nil || plain == "" {
		return ErrTOTPNotConfigured
	}
	if !totp.Verify(plain, code, time.Now()) {
		b.FailedCount++
		if b.FailedCount >= s.maxFail {
			b.LockedUntil = time.Now().Add(s.lockFor)
			b.FailedCount = 0
		}
		_ = s.totp.Save(ctx, b)
		if s.audit != nil {
			s.audit.Log(ctx, "USER", userID, "totp.confirm", "failed", ip, nil)
		}
		return ErrTOTPInvalid
	}
	b.Enabled = true
	b.FailedCount = 0
	b.LockedUntil = time.Time{}
	if err := s.totp.Save(ctx, b); err != nil {
		return err
	}
	if s.audit != nil {
		s.audit.Log(ctx, "USER", userID, "totp.activate", "success", ip, map[string]string{"ref": b.SecretRef})
	}
	return nil
}

// DisableTOTP 关闭指定用户的 TOTP 双因子绑定。
func (s *Service) DisableTOTP(ctx context.Context, userID, ip string) error {
	b, err := s.totp.Get(ctx, userID)
	if err != nil || b == nil {
		return nil
	}
	b.Enabled = false
	b.SecretRef = ""
	b.FailedCount = 0
	b.LockedUntil = time.Time{}
	if err := s.totp.Save(ctx, b); err != nil {
		if s.audit != nil {
			s.audit.Log(ctx, "USER", userID, "totp.disable", "failed", ip, map[string]string{"error": err.Error()})
		}
		return err
	}
	if s.audit != nil {
		s.audit.Log(ctx, "USER", userID, "totp.disable", "success", ip, nil)
	}
	return nil
}

// TOTPEnabled 用户是否已启用 TOTP。
func (s *Service) TOTPEnabled(ctx context.Context, userID string) bool {
	b, _ := s.totp.Get(ctx, userID)
	return b != nil && b.Enabled && b.SecretRef != ""
}

// VerifyUserTOTP 校验用户 TOTP（step-up / CRITICAL）。
func (s *Service) VerifyUserTOTP(ctx context.Context, userID, code, ip string) error {
	return s.verifyTOTP(ctx, userID, code, ip)
}

// Get / List。
func (s *Service) Get(ctx context.Context, id string) (*Request, error) {
	return s.store.Get(ctx, id)
}
func (s *Service) List(ctx context.Context) ([]*Request, error) {
	return s.store.List(ctx)
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// MemoryStore / MemoryTOTP。
type MemoryStore struct {
	mu   sync.RWMutex
	byID map[string]*Request
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: map[string]*Request{}}
}

func (m *MemoryStore) Save(_ context.Context, r *Request) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *r
	if r.Payload != nil {
		cp.Payload = map[string]string{}
		for k, v := range r.Payload {
			cp.Payload[k] = v
		}
	}
	m.byID[r.ID] = &cp
	return nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (*Request, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (m *MemoryStore) List(_ context.Context) ([]*Request, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Request, 0, len(m.byID))
	for _, r := range m.byID {
		cp := *r
		out = append(out, &cp)
	}
	return out, nil
}

type MemoryTOTP struct {
	mu sync.RWMutex
	m  map[string]*TOTPBinding
}

func NewMemoryTOTP() *MemoryTOTP {
	return &MemoryTOTP{m: map[string]*TOTPBinding{}}
}

func (m *MemoryTOTP) Get(_ context.Context, userID string) (*TOTPBinding, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.m[userID]
	if !ok {
		return nil, nil
	}
	cp := *b
	return &cp, nil
}

func (m *MemoryTOTP) Save(_ context.Context, b *TOTPBinding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *b
	m.m[b.UserID] = &cp
	return nil
}

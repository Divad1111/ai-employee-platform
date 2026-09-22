// Package auth 实现 Admin 登录、会话与 RBAC。
// 密码使用 bcrypt；含 IP/用户限流与临时锁定。
// 设计依据：设计文档 §78、§79、§27。
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// 常见错误。
var (
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	ErrUserLocked         = errors.New("账户暂时锁定，请稍后重试")
	ErrUnauthorized       = errors.New("未登录或会话无效")
	ErrForbidden          = errors.New("权限不足")
	ErrAlreadyInitialized = errors.New("系统已初始化，首次部署设置已关闭")
	ErrWeakPassword       = errors.New("密码长度至少需 8 个字符")
	ErrInvalidUsername    = errors.New("用户名长度需在 3 至 32 个字符之间")
)

// User 表示管理员用户。
type User struct {
	ID             string
	Username       string
	PasswordHash   string
	DisplayName    string
	Roles          []string
	FailedAttempts int
	LockedUntil    time.Time
}

// Session 表示登录会话。
type Session struct {
	Token       string
	UserID      string
	Username    string
	Roles       []string
	ExpiresAt   time.Time
	StepUpUntil time.Time // 高风险操作二次认证有效期
}

// UserStore 用户持久化抽象（M2 可用内存实现）。
type UserStore interface {
	FindByUsername(ctx context.Context, username string) (*User, error)
	Create(ctx context.Context, u *User) error
	Update(ctx context.Context, u *User) error
	ListPermissions(ctx context.Context, roles []string) ([]string, error)
	Count(ctx context.Context) (int, error)
	IsInitialized(ctx context.Context) (bool, error)
}

// SessionStore 会话存储。
type SessionStore interface {
	Save(ctx context.Context, s *Session) error
	Get(ctx context.Context, token string) (*Session, error)
	Delete(ctx context.Context, token string) error
}

// Auditor 审计写入。
type Auditor interface {
	Log(ctx context.Context, actorType, actorID, action, result, ip string, meta map[string]string)
}

// Service 认证服务。
type Service struct {
	users    UserStore
	sessions SessionStore
	audit    Auditor
	limiter  *RateLimiter

	sessionTTL time.Duration
	maxFail    int
	lockFor    time.Duration
}

// NewService 创建认证服务。
func NewService(users UserStore, sessions SessionStore, audit Auditor) *Service {
	return &Service{
		users:      users,
		sessions:   sessions,
		audit:      audit,
		limiter:    NewRateLimiter(20, time.Minute),
		sessionTTL: 12 * time.Hour,
		maxFail:    5,
		lockFor:    15 * time.Minute,
	}
}

// Login 校验凭证并创建会话。
func (s *Service) Login(ctx context.Context, username, password, ip string) (*Session, error) {
	if !s.limiter.Allow("ip:"+ip) || !s.limiter.Allow("user:"+username) {
		s.audit.Log(ctx, "USER", username, "login", "rate_limited", ip, nil)
		return nil, ErrInvalidCredentials
	}
	u, err := s.users.FindByUsername(ctx, username)
	if err != nil || u == nil {
		s.audit.Log(ctx, "USER", username, "login", "failed", ip, nil)
		return nil, ErrInvalidCredentials
	}
	if !u.LockedUntil.IsZero() && time.Now().Before(u.LockedUntil) {
		s.audit.Log(ctx, "USER", u.ID, "login", "locked", ip, nil)
		return nil, ErrUserLocked
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		u.FailedAttempts++
		if u.FailedAttempts >= s.maxFail {
			u.LockedUntil = time.Now().Add(s.lockFor)
			u.FailedAttempts = 0
		}
		_ = s.users.Update(ctx, u)
		s.audit.Log(ctx, "USER", u.ID, "login", "failed", ip, nil)
		return nil, ErrInvalidCredentials
	}
	u.FailedAttempts = 0
	u.LockedUntil = time.Time{}
	_ = s.users.Update(ctx, u)

	tok, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	sess := &Session{
		Token:     tok,
		UserID:    u.ID,
		Username:  u.Username,
		Roles:     append([]string{}, u.Roles...),
		ExpiresAt: time.Now().Add(s.sessionTTL),
	}
	if err := s.sessions.Save(ctx, sess); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, "USER", u.ID, "login", "success", ip, map[string]string{"username": u.Username})
	return sess, nil
}

// Logout 销毁会话。
func (s *Service) Logout(ctx context.Context, token, ip string) error {
	sess, _ := s.sessions.Get(ctx, token)
	_ = s.sessions.Delete(ctx, token)
	if sess != nil {
		s.audit.Log(ctx, "USER", sess.UserID, "logout", "success", ip, nil)
	}
	return nil
}

// Authenticate 校验会话并返回会话信息。
func (s *Service) Authenticate(ctx context.Context, token string) (*Session, error) {
	if token == "" {
		return nil, ErrUnauthorized
	}
	sess, err := s.sessions.Get(ctx, token)
	if err != nil || sess == nil || time.Now().After(sess.ExpiresAt) {
		return nil, ErrUnauthorized
	}
	return sess, nil
}

// Authorize 检查会话是否具备任一权限码。
func (s *Service) Authorize(ctx context.Context, sess *Session, need string) error {
	if sess == nil {
		return ErrUnauthorized
	}
	perms, err := s.users.ListPermissions(ctx, sess.Roles)
	if err != nil {
		return err
	}
	for _, p := range perms {
		if p == need || p == "*" {
			return nil
		}
	}
	return ErrForbidden
}

// ErrStepUpRequired 需要二次认证。
var ErrStepUpRequired = errors.New("需要二次认证（step-up）")

// HasStepUp 会话是否仍在 step-up 窗口内。
func (s *Service) HasStepUp(sess *Session) bool {
	return sess != nil && !sess.StepUpUntil.IsZero() && time.Now().Before(sess.StepUpUntil)
}

// RequireStepUp 未 step-up 返回 ErrStepUpRequired。
func (s *Service) RequireStepUp(sess *Session) error {
	if s.HasStepUp(sess) {
		return nil
	}
	return ErrStepUpRequired
}

// StepUp 密码（及可选 TOTP 校验由调用方完成）后延长 step-up 窗口。
func (s *Service) StepUp(ctx context.Context, token, password, ip string, ttl time.Duration) (*Session, error) {
	sess, err := s.Authenticate(ctx, token)
	if err != nil {
		return nil, err
	}
	u, err := s.users.FindByUsername(ctx, sess.Username)
	if err != nil || u == nil {
		return nil, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		s.audit.Log(ctx, "USER", sess.UserID, "auth.step_up", "failed", ip, nil)
		return nil, ErrInvalidCredentials
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	sess.StepUpUntil = time.Now().Add(ttl)
	if err := s.sessions.Save(ctx, sess); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, "USER", sess.UserID, "auth.step_up", "success", ip, nil)
	return sess, nil
}

// VerifyPassword 校验用户密码（TOTP 注册等场景）。
func (s *Service) VerifyPassword(ctx context.Context, username, password string) error {
	u, err := s.users.FindByUsername(ctx, username)
	if err != nil || u == nil {
		return ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return ErrInvalidCredentials
	}
	return nil
}

// IsInitialized 检查系统是否已初始化管理员。
func (s *Service) IsInitialized(ctx context.Context) (bool, error) {
	return s.users.IsInitialized(ctx)
}

// InitAdmin 执行首次部署管理员注册，并返回登录会话。
func (s *Service) InitAdmin(ctx context.Context, username, password, display, ip string) (*Session, error) {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 32 {
		return nil, ErrInvalidUsername
	}
	if len(password) < 8 {
		return nil, ErrWeakPassword
	}
	initialized, err := s.users.IsInitialized(ctx)
	if err != nil {
		return nil, err
	}
	if initialized {
		return nil, ErrAlreadyInitialized
	}

	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	if display == "" {
		display = "系统管理员"
	}
	u := &User{
		ID:           newUUID(),
		Username:     username,
		PasswordHash: hash,
		DisplayName:  display,
		Roles:        []string{"SUPER_ADMIN", "ADMIN"},
	}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, err
	}

	tok, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	sess := &Session{
		Token:     tok,
		UserID:    u.ID,
		Username:  u.Username,
		Roles:     append([]string{}, u.Roles...),
		ExpiresAt: time.Now().Add(s.sessionTTL),
	}
	if err := s.sessions.Save(ctx, sess); err != nil {
		return nil, err
	}

	s.audit.Log(ctx, "USER", u.ID, "system.bootstrap", "success", ip, map[string]string{
		"username":     u.Username,
		"display_name": display,
		"roles":        "SUPER_ADMIN,ADMIN",
	})
	return sess, nil
}

// HashPassword 使用 bcrypt 生成密码哈希。
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// RateLimiter 简易滑动窗口限流。
type RateLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	attempts map[string][]time.Time
}

// NewRateLimiter 创建限流器。
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, attempts: map[string][]time.Time{}}
}

// Allow 返回是否允许本次尝试。
func (r *RateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	cut := now.Add(-r.window)
	arr := r.attempts[key]
	alive := arr[:0]
	for _, t := range arr {
		if t.After(cut) {
			alive = append(alive, t)
		}
	}
	if len(alive) >= r.limit {
		r.attempts[key] = alive
		return false
	}
	r.attempts[key] = append(alive, now)
	return true
}

// MemoryUserStore 内存用户库（开发/测试）。
type MemoryUserStore struct {
	mu    sync.RWMutex
	users map[string]*User
	perms map[string][]string // role -> permissions
}

// NewMemoryUserStore 创建空内存用户库。
func NewMemoryUserStore() *MemoryUserStore {
	return &MemoryUserStore{
		users: map[string]*User{},
		perms: map[string][]string{
			"SUPER_ADMIN": {"*"},
			"ADMIN": {
				"employee.read", "employee.write", "employee.delete",
				"workstation.read", "workstation.write",
				"workspace.read", "workspace.write",
				"session.read", "session.write",
				"job.read", "job.write", "job.cancel",
				"message.read", "message.write",
				"audit.read",
				"approval.read", "approval.approve",
				"secret.read", "secret.write",
				"system.read", "system.write",
				"enrollment.write",
				"workflow.read", "workflow.write", "workflow.delete", "workflow.grant",
			},
			"OPERATOR": {
				"employee.read", "workstation.read", "workspace.read",
				"session.read", "job.read", "job.write", "job.cancel",
				"message.read", "message.write", "approval.read",
				"workflow.read", "workflow.grant",
			},
			"VIEWER": {
				"employee.read", "workstation.read", "workspace.read",
				"session.read", "job.read", "message.read",
				"approval.read", "system.read", "audit.read",
				"workflow.read",
			},
		},
	}
}

// SeedAdmin 写入初始管理员。
func (m *MemoryUserStore) SeedAdmin(username, password, display string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[username] = &User{
		ID:           newUUID(),
		Username:     username,
		PasswordHash: hash,
		DisplayName:  display,
		Roles:        []string{"ADMIN"},
	}
	return nil
}

// FindByUsername 按用户名查找。
func (m *MemoryUserStore) FindByUsername(_ context.Context, username string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u := m.users[username]
	if u == nil {
		return nil, nil
	}
	cp := *u
	cp.Roles = append([]string{}, u.Roles...)
	return &cp, nil
}

// Create 新建用户。
func (m *MemoryUserStore) Create(_ context.Context, u *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.users[u.Username]; exists {
		return errors.New("用户名已存在")
	}
	cp := *u
	cp.Roles = append([]string{}, u.Roles...)
	m.users[u.Username] = &cp
	return nil
}

// Update 更新用户。
func (m *MemoryUserStore) Update(_ context.Context, u *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *u
	cp.Roles = append([]string{}, u.Roles...)
	m.users[u.Username] = &cp
	return nil
}

// Count 返回用户总数。
func (m *MemoryUserStore) Count(_ context.Context) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.users), nil
}

// IsInitialized 检查系统是否已有管理员角色用户。
func (m *MemoryUserStore) IsInitialized(_ context.Context) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		for _, r := range u.Roles {
			if r == "ADMIN" || r == "SUPER_ADMIN" {
				return true, nil
			}
		}
	}
	return false, nil
}

// ListPermissions 汇总角色权限。
func (m *MemoryUserStore) ListPermissions(_ context.Context, roles []string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := map[string]struct{}{}
	var out []string
	for _, r := range roles {
		for _, p := range m.perms[r] {
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out, nil
}

// MemorySessionStore 内存会话。
type MemorySessionStore struct {
	mu   sync.RWMutex
	data map[string]*Session
}

// NewMemorySessionStore 创建会话存储。
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{data: map[string]*Session{}}
}

// Save 保存会话。
func (m *MemorySessionStore) Save(_ context.Context, s *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.data[s.Token] = &cp
	return nil
}

// Get 读取会话。
func (m *MemorySessionStore) Get(_ context.Context, token string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := m.data[token]
	if s == nil {
		return nil, nil
	}
	cp := *s
	return &cp, nil
}

// Delete 删除会话。
func (m *MemorySessionStore) Delete(_ context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, token)
	return nil
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}


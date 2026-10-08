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
	"sort"
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
	ErrUserDisabled       = errors.New("账户已禁用")
)

// User 表示管理员用户。
type User struct {
	ID             string
	Username       string
	PasswordHash   string
	DisplayName    string
	Email          string
	Status         string // active | disabled | pending
	Roles          []string
	FailedAttempts int
	LockedUntil    time.Time
	LastLoginAt    time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
	StatusPending  = "pending"
)

// PermissionGrant 权限码 + 数据 Scope。
type PermissionGrant struct {
	Code  string `json:"code"`
	Scope string `json:"scope"` // ALL | OWN | ASSIGNED | NONE
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

// UserStore 用户持久化抽象。
type UserStore interface {
	FindByUsername(ctx context.Context, username string) (*User, error)
	FindByID(ctx context.Context, id string) (*User, error)
	List(ctx context.Context) ([]*User, error)
	Create(ctx context.Context, u *User) error
	Update(ctx context.Context, u *User) error
	SetRoles(ctx context.Context, userID string, roles []string) error
	ListPermissionGrants(ctx context.Context, roles []string) ([]PermissionGrant, error)
	ListPermissions(ctx context.Context, roles []string) ([]string, error)
	ListRoles(ctx context.Context) ([]RoleInfo, error)
	ListAllPermissions(ctx context.Context) ([]PermInfo, error)
	SetRolePermissionScope(ctx context.Context, roleName, permCode, scope string) error
	CreateRole(ctx context.Context, name, description string, grants []PermissionGrant) error
	DeleteRole(ctx context.Context, name string) error
	SoftDelete(ctx context.Context, id string) error
	Count(ctx context.Context) (int, error)
	IsInitialized(ctx context.Context) (bool, error)
}

// BuiltInRoles 系统内置角色，不可删除。
var BuiltInRoles = map[string]struct{}{
	"SUPER_ADMIN": {},
	"ADMIN":       {},
	"OPERATOR":    {},
	"USER":        {},
	"VIEWER":      {},
}


// IsBuiltInRole 是否内置角色。
func IsBuiltInRole(name string) bool {
	_, ok := BuiltInRoles[strings.ToUpper(strings.TrimSpace(name))]
	return ok
}

// RoleInfo 角色摘要。
type RoleInfo struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Grants      []PermissionGrant `json:"grants"`
}

// PermInfo 权限码元数据。
type PermInfo struct {
	Code        string `json:"code"`
	Description string `json:"description"`
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
	if u.Status == StatusDisabled {
		s.audit.Log(ctx, "USER", u.ID, "login", "disabled", ip, nil)
		return nil, ErrUserDisabled
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
	u.LastLoginAt = time.Now().UTC()
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

// Authenticate 校验会话并返回会话信息；禁用用户拒绝。
func (s *Service) Authenticate(ctx context.Context, token string) (*Session, error) {
	if token == "" {
		return nil, ErrUnauthorized
	}
	sess, err := s.sessions.Get(ctx, token)
	if err != nil || sess == nil || time.Now().After(sess.ExpiresAt) {
		return nil, ErrUnauthorized
	}
	u, _ := s.users.FindByID(ctx, sess.UserID)
	if u == nil {
		u, _ = s.users.FindByUsername(ctx, sess.Username)
	}
	if u != nil && u.Status == StatusDisabled {
		return nil, ErrUserDisabled
	}
	return sess, nil
}

// Authorize 检查会话是否具备任一权限码。
func (s *Service) Authorize(ctx context.Context, sess *Session, need string) error {
	if sess == nil {
		return ErrUnauthorized
	}
	grants, err := s.users.ListPermissionGrants(ctx, sess.Roles)
	if err != nil {
		return err
	}
	for _, g := range grants {
		if g.Code == need || g.Code == "*" {
			return nil
		}
	}
	return ErrForbidden
}

// PermissionGrants 返回会话全部授权（含 scope）。
func (s *Service) PermissionGrants(ctx context.Context, sess *Session) ([]PermissionGrant, error) {
	if sess == nil {
		return nil, ErrUnauthorized
	}
	return s.users.ListPermissionGrants(ctx, sess.Roles)
}

// Users 暴露用户存储（管理 API）。
func (s *Service) Users() UserStore { return s.users }

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
		Status:       StatusActive,
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
	mu     sync.RWMutex
	users  map[string]*User             // username -> user
	byID   map[string]string            // id -> username
	grants map[string][]PermissionGrant // role -> grants
	descs  map[string]string            // role -> description
}

func defaultRoleGrants() map[string][]PermissionGrant {
	all := func(codes ...string) []PermissionGrant {
		out := make([]PermissionGrant, 0, len(codes))
		for _, c := range codes {
			out = append(out, PermissionGrant{Code: c, Scope: "ALL"})
		}
		return out
	}
	own := func(codes ...string) []PermissionGrant {
		out := make([]PermissionGrant, 0, len(codes))
		for _, c := range codes {
			out = append(out, PermissionGrant{Code: c, Scope: "OWN"})
		}
		return out
	}
	adminCodes := []string{
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
		"automation.read", "automation.write",
		"user.read", "user.create", "user.update", "user.disable", "user.delete",
		"role.read", "role.create", "role.update", "role.delete",
		"quota.read", "quota.update",
	}
	return map[string][]PermissionGrant{
		"SUPER_ADMIN": {{Code: "*", Scope: "ALL"}},
		"ADMIN":       all(adminCodes...),
		"OPERATOR": append(own(
			"employee.read", "employee.write", "workstation.read", "workspace.read",
			"session.read", "job.read", "job.write", "job.cancel",
			"message.read", "message.write", "approval.read",
			"workflow.read", "workflow.grant",
			"automation.read", "quota.read",
		)),
		"USER": own(adminCodes...),
		"VIEWER": own(
			"employee.read", "workstation.read", "workspace.read",
			"session.read", "job.read", "message.read",
			"approval.read", "audit.read",
			"workflow.read", "automation.read",
		),
	}
}

// NewMemoryUserStore 创建空内存用户库。
func NewMemoryUserStore() *MemoryUserStore {
	return &MemoryUserStore{
		users:  map[string]*User{},
		byID:   map[string]string{},
		grants: defaultRoleGrants(),
		descs: map[string]string{
			"SUPER_ADMIN": "超级管理员",
			"ADMIN":       "管理员",
			"OPERATOR":    "操作员",
			"USER":        "普通用户",
			"VIEWER":      "只读",
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
	id := newUUID()
	m.users[username] = &User{
		ID:           id,
		Username:     username,
		PasswordHash: hash,
		DisplayName:  display,
		Status:       StatusActive,
		Roles:        []string{"ADMIN"},
	}
	m.byID[id] = username
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

// FindByID 按 ID 查找。
func (m *MemoryUserStore) FindByID(_ context.Context, id string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	uname := m.byID[id]
	if uname == "" {
		return nil, nil
	}
	u := m.users[uname]
	if u == nil {
		return nil, nil
	}
	cp := *u
	cp.Roles = append([]string{}, u.Roles...)
	return &cp, nil
}

// List 列出全部用户。
func (m *MemoryUserStore) List(_ context.Context) ([]*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*User, 0, len(m.users))
	for _, u := range m.users {
		cp := *u
		cp.Roles = append([]string{}, u.Roles...)
		out = append(out, &cp)
	}
	return out, nil
}

// Create 新建用户。
func (m *MemoryUserStore) Create(_ context.Context, u *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.users[u.Username]; exists {
		return errors.New("用户名已存在")
	}
	if u.Status == "" {
		u.Status = StatusActive
	}
	if u.ID == "" {
		u.ID = newUUID()
	}
	cleanRoles := make([]string, 0, len(u.Roles))
	for _, r := range u.Roles {
		r = strings.TrimSpace(r)
		if r != "" {
			cleanRoles = append(cleanRoles, r)
		}
	}
	if len(cleanRoles) == 0 {
		cleanRoles = []string{"USER"}
	}
	u.Roles = cleanRoles
	cp := *u
	cp.Roles = cleanRoles
	m.users[u.Username] = &cp
	m.byID[u.ID] = u.Username
	return nil
}


// Update 更新用户。
func (m *MemoryUserStore) Update(_ context.Context, u *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old := m.users[u.Username]
	if old == nil {
		// 允许按 ID 更新用户名以外字段：先找 byID
		if uname := m.byID[u.ID]; uname != "" {
			old = m.users[uname]
			u.Username = uname
		}
	}
	if old == nil {
		return errors.New("用户不存在")
	}
	cp := *u
	cp.Roles = append([]string{}, u.Roles...)
	if len(cp.Roles) == 0 {
		cp.Roles = append([]string{}, old.Roles...)
	}
	m.users[u.Username] = &cp
	m.byID[u.ID] = u.Username
	return nil
}

// SetRoles 设置用户角色。
func (m *MemoryUserStore) SetRoles(_ context.Context, userID string, roles []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	uname := m.byID[userID]
	if uname == "" {
		return errors.New("用户不存在")
	}
	u := m.users[uname]
	cleanRoles := make([]string, 0, len(roles))
	for _, r := range roles {
		r = strings.TrimSpace(r)
		if r != "" {
			cleanRoles = append(cleanRoles, r)
		}
	}
	if len(cleanRoles) == 0 {
		cleanRoles = []string{"USER"}
	}
	u.Roles = cleanRoles
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

// ListPermissionGrants 汇总角色权限与 Scope。
func (m *MemoryUserStore) ListPermissionGrants(_ context.Context, roles []string) ([]PermissionGrant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := map[string]PermissionGrant{}
	for _, r := range roles {
		for _, g := range m.grants[r] {
			if prev, ok := seen[g.Code]; ok {
				if scopeRank(g.Scope) > scopeRank(prev.Scope) {
					seen[g.Code] = g
				}
				continue
			}
			seen[g.Code] = g
		}
	}
	out := make([]PermissionGrant, 0, len(seen))
	for _, g := range seen {
		out = append(out, g)
	}
	return out, nil
}

func scopeRank(s string) int {
	switch strings.ToUpper(s) {
	case "ALL":
		return 4
	case "ASSIGNED":
		return 3
	case "OWN":
		return 2
	default:
		return 1
	}
}

// ListPermissions 汇总角色权限码。
func (m *MemoryUserStore) ListPermissions(ctx context.Context, roles []string) ([]string, error) {
	grants, err := m.ListPermissionGrants(ctx, roles)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(grants))
	for _, g := range grants {
		out = append(out, g.Code)
	}
	return out, nil
}

// ListRoles 列出角色及授权。
func (m *MemoryUserStore) ListRoles(_ context.Context) ([]RoleInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.grants))
	for n := range m.grants {
		names = append(names, n)
	}
	order := map[string]int{"SUPER_ADMIN": 0, "ADMIN": 1, "OPERATOR": 2, "USER": 3, "VIEWER": 4}
	sort.Slice(names, func(i, j int) bool {
		oi, oki := order[names[i]]
		oj, okj := order[names[j]]
		if !oki {
			oi = 100
		}
		if !okj {
			oj = 100
		}
		if oi != oj {
			return oi < oj
		}
		return names[i] < names[j]
	})
	out := make([]RoleInfo, 0, len(names))
	for _, n := range names {
		gs := append([]PermissionGrant{}, m.grants[n]...)
		out = append(out, RoleInfo{Name: n, Description: m.descs[n], Grants: gs})
	}
	return out, nil
}

// CreateRole 新建角色并写入初始授权。
func (m *MemoryUserStore) CreateRole(_ context.Context, name, description string, grants []PermissionGrant) error {
	name = strings.ToUpper(strings.TrimSpace(name))
	if name == "" {
		return errors.New("角色标识不能为空")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.grants[name]; exists {
		return errors.New("角色已存在")
	}
	cp := make([]PermissionGrant, 0, len(grants))
	for _, g := range grants {
		code := strings.TrimSpace(g.Code)
		if code == "" {
			continue
		}
		scope := strings.ToUpper(strings.TrimSpace(g.Scope))
		if scope == "" {
			scope = "NONE"
		}
		cp = append(cp, PermissionGrant{Code: code, Scope: scope})
	}
	m.grants[name] = cp
	if description == "" {
		description = name
	}
	m.descs[name] = description
	return nil
}

// DeleteRole 删除自定义角色（内置角色禁止）。
func (m *MemoryUserStore) DeleteRole(_ context.Context, name string) error {
	name = strings.ToUpper(strings.TrimSpace(name))
	if name == "" {
		return errors.New("角色标识不能为空")
	}
	if IsBuiltInRole(name) {
		return errors.New("内置角色不可删除")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.grants[name]; !ok {
		return errors.New("角色不存在")
	}
	for _, u := range m.users {
		for _, r := range u.Roles {
			if strings.EqualFold(r, name) {
				return errors.New("仍有用户使用该角色，请先调整用户角色")
			}
		}
	}
	delete(m.grants, name)
	delete(m.descs, name)
	return nil
}

// SoftDelete 软删除用户。
func (m *MemoryUserStore) SoftDelete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	uname := m.byID[id]
	if uname == "" {
		return errors.New("用户不存在")
	}
	delete(m.users, uname)
	delete(m.byID, id)
	return nil
}

// ListAllPermissions 权限码目录。
func (m *MemoryUserStore) ListAllPermissions(_ context.Context) ([]PermInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := map[string]struct{}{}
	var out []PermInfo
	for _, gs := range m.grants {
		for _, g := range gs {
			if g.Code == "*" {
				continue
			}
			if _, ok := seen[g.Code]; ok {
				continue
			}
			seen[g.Code] = struct{}{}
			out = append(out, PermInfo{Code: g.Code, Description: g.Code})
		}
	}
	return out, nil
}

// SetRolePermissionScope 更新角色某权限的 Scope。
func (m *MemoryUserStore) SetRolePermissionScope(_ context.Context, roleName, permCode, scope string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	gs := m.grants[roleName]
	found := false
	for i := range gs {
		if gs[i].Code == permCode {
			gs[i].Scope = strings.ToUpper(scope)
			found = true
			break
		}
	}
	if !found {
		gs = append(gs, PermissionGrant{Code: permCode, Scope: strings.ToUpper(scope)})
	}
	m.grants[roleName] = gs
	return nil
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


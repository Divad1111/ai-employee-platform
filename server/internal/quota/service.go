// Package quota Token/Request 配额（§12–14、§33）。
package quota

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/idgen"
)

var (
	ErrExceeded = errors.New("quota_exceeded")
	ErrNotFound = errors.New("配额策略不存在")
)

const (
	TypeUser        = "USER"
	TypeUserBonus   = "USER_BONUS" // 用户额外：加在「例外或角色」基数之上
	TypeWorkstation = "WORKSTATION"
	TypeEmployee    = "DIGITAL_EMPLOYEE"
	TypeRole        = "ROLE" // 角色预设：resource_id=角色名；个人 USER 例外会替换它
	PeriodMonthly   = "MONTHLY"
)

// EffectiveQuota 当前用户的有效月度限额拆解。
// 用户例外替换角色基数；用户额外再叠加在基数上。基数为不限（限额 ≤ 0）时，有效限额仍为不限。
type EffectiveQuota struct {
	Source                string
	SourceRole            string
	RoleTokenLimit        int64
	RoleRequestLimit      int64
	ExceptionTokenLimit   int64
	ExceptionRequestLimit int64
	ExtraTokenLimit       int64
	ExtraRequestLimit     int64
	TokenLimit            int64
	RequestLimit          int64
	HasException          bool
	HasExtra              bool
	Unlimited             bool
}

// Policy 配额策略。
type Policy struct {
	ID               string    `json:"id"`
	ResourceType     string    `json:"resource_type"`
	ResourceID       string    `json:"resource_id"`
	PeriodType       string    `json:"period_type"`
	TokenLimit       int64     `json:"token_limit"`
	RequestLimit     int64     `json:"request_limit"`
	ConcurrencyLimit int       `json:"concurrency_limit"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// WSUserQuota User 在某 Workstation 上的配额。
type WSUserQuota struct {
	ID               string    `json:"id"`
	WorkstationID    string    `json:"workstation_id"`
	UserID           string    `json:"user_id"`
	PeriodType       string    `json:"period_type"`
	TokenLimit       int64     `json:"token_limit"`
	RequestLimit     int64     `json:"request_limit"`
	ConcurrencyLimit int       `json:"concurrency_limit"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Usage 周期用量。
type Usage struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	PeriodType   string `json:"period_type"`
	PeriodKey    string `json:"period_key"`
	TokensUsed   int64  `json:"tokens_used"`
	RequestsUsed int64  `json:"requests_used"`
}

// Store 配额存储。
type Store interface {
	UpsertPolicy(ctx context.Context, p *Policy) error
	DeletePolicy(ctx context.Context, resourceType, resourceID, period string) error
	GetPolicy(ctx context.Context, resourceType, resourceID, period string) (*Policy, error)
	ListPolicies(ctx context.Context) ([]*Policy, error)
	UpsertWSUserQuota(ctx context.Context, q *WSUserQuota) error
	ListWSUserQuotas(ctx context.Context, workstationID string) ([]*WSUserQuota, error)
	AddUsage(ctx context.Context, resourceType, resourceID, period string, tokens, requests int64) (*Usage, error)
	GetUsage(ctx context.Context, resourceType, resourceID, period string) (*Usage, error)
}

// MemoryStore 内存实现。
type MemoryStore struct {
	mu       sync.RWMutex
	policies map[string]*Policy
	wsUser   map[string]*WSUserQuota
	usage    map[string]*Usage
}

// NewMemoryStore 创建（含内置角色月度预设）。
func NewMemoryStore() *MemoryStore {
	m := &MemoryStore{
		policies: map[string]*Policy{},
		wsUser:   map[string]*WSUserQuota{},
		usage:    map[string]*Usage{},
	}
	_ = m.UpsertPolicy(context.Background(), &Policy{
		ResourceType: TypeRole, ResourceID: "VIEWER", PeriodType: PeriodMonthly,
		TokenLimit: 1_000_000, Enabled: true,
	})
	_ = m.UpsertPolicy(context.Background(), &Policy{
		ResourceType: TypeRole, ResourceID: "OPERATOR", PeriodType: PeriodMonthly,
		TokenLimit: 5_000_000, Enabled: true,
	})
	_ = m.UpsertPolicy(context.Background(), &Policy{
		ResourceType: TypeRole, ResourceID: "ADMIN", PeriodType: PeriodMonthly,
		TokenLimit: 50_000_000, Enabled: true,
	})
	return m
}

func polKey(t, id, period string) string { return t + "|" + id + "|" + period }
func wuKey(ws, user, period string) string {
	return ws + "|" + user + "|" + period
}

func periodKey(period string) string {
	now := time.Now().UTC()
	switch period {
	case "DAILY":
		return now.Format("2006-01-02")
	case "WEEKLY":
		y, w := now.ISOWeek()
		return time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, (w-1)*7).Format("2006-01-02")
	case "YEARLY":
		return now.Format("2006")
	default:
		return now.Format("2006-01")
	}
}

func (m *MemoryStore) UpsertPolicy(_ context.Context, p *Policy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if p.ID == "" {
		p.ID = idgen.New("QTA")
		p.CreatedAt = now
	}
	if p.PeriodType == "" {
		p.PeriodType = PeriodMonthly
	}
	p.UpdatedAt = now
	cp := *p
	m.policies[polKey(p.ResourceType, p.ResourceID, p.PeriodType)] = &cp
	return nil
}

func (m *MemoryStore) DeletePolicy(_ context.Context, resourceType, resourceID, period string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if period == "" {
		period = PeriodMonthly
	}
	k := polKey(resourceType, resourceID, period)
	if _, ok := m.policies[k]; !ok {
		return ErrNotFound
	}
	delete(m.policies, k)
	return nil
}

func (m *MemoryStore) GetPolicy(_ context.Context, resourceType, resourceID, period string) (*Policy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if period == "" {
		period = PeriodMonthly
	}
	p := m.policies[polKey(resourceType, resourceID, period)]
	if p == nil {
		return nil, ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (m *MemoryStore) ListPolicies(_ context.Context) ([]*Policy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Policy, 0, len(m.policies))
	for _, p := range m.policies {
		cp := *p
		out = append(out, &cp)
	}
	return out, nil
}

func (m *MemoryStore) UpsertWSUserQuota(_ context.Context, q *WSUserQuota) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if q.ID == "" {
		q.ID = idgen.New("WQU")
		q.CreatedAt = now
	}
	if q.PeriodType == "" {
		q.PeriodType = PeriodMonthly
	}
	q.UpdatedAt = now
	cp := *q
	m.wsUser[wuKey(q.WorkstationID, q.UserID, q.PeriodType)] = &cp
	return nil
}

func (m *MemoryStore) ListWSUserQuotas(_ context.Context, workstationID string) ([]*WSUserQuota, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*WSUserQuota
	for _, q := range m.wsUser {
		if workstationID == "" || q.WorkstationID == workstationID {
			cp := *q
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *MemoryStore) AddUsage(_ context.Context, resourceType, resourceID, period string, tokens, requests int64) (*Usage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if period == "" {
		period = PeriodMonthly
	}
	pk := periodKey(period)
	k := polKey(resourceType, resourceID, period) + "|" + pk
	u := m.usage[k]
	if u == nil {
		u = &Usage{ResourceType: resourceType, ResourceID: resourceID, PeriodType: period, PeriodKey: pk}
		m.usage[k] = u
	}
	u.TokensUsed += tokens
	u.RequestsUsed += requests
	cp := *u
	return &cp, nil
}

func (m *MemoryStore) GetUsage(_ context.Context, resourceType, resourceID, period string) (*Usage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if period == "" {
		period = PeriodMonthly
	}
	pk := periodKey(period)
	k := polKey(resourceType, resourceID, period) + "|" + pk
	u := m.usage[k]
	if u == nil {
		return &Usage{ResourceType: resourceType, ResourceID: resourceID, PeriodType: period, PeriodKey: pk}, nil
	}
	cp := *u
	return &cp, nil
}

// Service 配额检查。
type Service struct {
	store Store
}

// NewService 创建。
func NewService(store Store) *Service {
	return &Service{store: store}
}

// Check 检查是否超限；limit=0 表示不限制。
func (s *Service) Check(ctx context.Context, resourceType, resourceID string, addTokens, addRequests int64) error {
	p, err := s.store.GetPolicy(ctx, resourceType, resourceID, PeriodMonthly)
	if err == ErrNotFound || p == nil || !p.Enabled {
		return nil
	}
	if err != nil {
		return err
	}
	u, err := s.store.GetUsage(ctx, resourceType, resourceID, PeriodMonthly)
	if err != nil {
		return err
	}
	if p.TokenLimit > 0 && u.TokensUsed+addTokens > p.TokenLimit {
		return ErrExceeded
	}
	if p.RequestLimit > 0 && u.RequestsUsed+addRequests > p.RequestLimit {
		return ErrExceeded
	}
	return nil
}

// ResolveUserPolicy 解析用户基数策略（不含额外）。
// 优先级：个人 USER 例外 > 用户角色中限额最高的 ROLE 预设 > 无限制。
// 用量始终按 USER:{userID} 累计（角色预设只决定上限，不共享池）。
func (s *Service) ResolveUserPolicy(ctx context.Context, userID string, roles []string) (*Policy, error) {
	p, err := s.store.GetPolicy(ctx, TypeUser, userID, PeriodMonthly)
	if err == nil && p != nil && p.Enabled {
		return p, nil
	}
	if err != nil && err != ErrNotFound {
		return nil, err
	}
	best, err := s.bestRolePolicy(ctx, roles)
	if err != nil {
		return nil, err
	}
	return best, nil
}

func (s *Service) bestRolePolicy(ctx context.Context, roles []string) (*Policy, error) {
	var best *Policy
	for _, role := range roles {
		rp, err := s.store.GetPolicy(ctx, TypeRole, role, PeriodMonthly)
		if err == ErrNotFound || rp == nil || !rp.Enabled {
			continue
		}
		if err != nil {
			return nil, err
		}
		if best == nil || rp.TokenLimit > best.TokenLimit ||
			(rp.TokenLimit == best.TokenLimit && rp.RequestLimit > best.RequestLimit) {
			cp := *rp
			best = &cp
		}
	}
	return best, nil
}

// ResolveEffective 计算有效限额：例外替换角色，额外再相加。
func (s *Service) ResolveEffective(ctx context.Context, userID string, roles []string) (*EffectiveQuota, error) {
	out := &EffectiveQuota{Source: "none", Unlimited: true}
	ex, err := s.optionalPolicy(ctx, TypeUser, userID)
	if err != nil {
		return nil, err
	}
	bonus, err := s.optionalPolicy(ctx, TypeUserBonus, userID)
	if err != nil {
		return nil, err
	}
	best, err := s.bestRolePolicy(ctx, roles)
	if err != nil {
		return nil, err
	}
	if best != nil {
		out.RoleTokenLimit = best.TokenLimit
		out.RoleRequestLimit = best.RequestLimit
		out.SourceRole = best.ResourceID
		out.Source = TypeRole
	}
	if ex != nil && ex.Enabled {
		out.HasException = true
		out.ExceptionTokenLimit = ex.TokenLimit
		out.ExceptionRequestLimit = ex.RequestLimit
		out.Source = TypeUser
	}
	if bonus != nil && bonus.Enabled {
		out.HasExtra = bonus.TokenLimit > 0 || bonus.RequestLimit > 0
		out.ExtraTokenLimit = bonus.TokenLimit
		out.ExtraRequestLimit = bonus.RequestLimit
	}

	tok, tokUnlimited := combineLimit(out.HasException, out.ExceptionTokenLimit, out.Source == TypeRole, out.RoleTokenLimit, out.ExtraTokenLimit)
	req, reqUnlimited := combineLimit(out.HasException, out.ExceptionRequestLimit, out.Source == TypeRole, out.RoleRequestLimit, out.ExtraRequestLimit)
	out.TokenLimit = tok
	out.RequestLimit = req
	out.Unlimited = tokUnlimited && reqUnlimited
	return out, nil
}

func (s *Service) optionalPolicy(ctx context.Context, resourceType, resourceID string) (*Policy, error) {
	p, err := s.store.GetPolicy(ctx, resourceType, resourceID, PeriodMonthly)
	if err == ErrNotFound || p == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !p.Enabled {
		return nil, nil
	}
	return p, nil
}

// combineLimit 基数（例外优先于角色）加上额外。基数限额 ≤ 0 表示不限，额外不再收紧。
// 没有任何基数时，额外单独作为上限。
func combineLimit(hasException bool, exceptionLimit int64, hasRole bool, roleLimit, extra int64) (limit int64, unlimited bool) {
	if extra < 0 {
		extra = 0
	}
	if hasException {
		if exceptionLimit <= 0 {
			return 0, true
		}
		return exceptionLimit + extra, false
	}
	if hasRole {
		if roleLimit <= 0 {
			return 0, true
		}
		return roleLimit + extra, false
	}
	if extra > 0 {
		return extra, false
	}
	return 0, true
}

// CheckUser 按有效限额检查；用量记在 USER 维度。
func (s *Service) CheckUser(ctx context.Context, userID string, roles []string, addTokens, addRequests int64) error {
	eff, err := s.ResolveEffective(ctx, userID, roles)
	if err != nil {
		return err
	}
	if eff == nil || eff.Unlimited {
		return nil
	}
	u, err := s.store.GetUsage(ctx, TypeUser, userID, PeriodMonthly)
	if err != nil {
		return err
	}
	if eff.TokenLimit > 0 && u.TokensUsed+addTokens > eff.TokenLimit {
		return ErrExceeded
	}
	if eff.RequestLimit > 0 && u.RequestsUsed+addRequests > eff.RequestLimit {
		return ErrExceeded
	}
	return nil
}

// Store 暴露底层存储（API 用）。
func (s *Service) Store() Store { return s.store }

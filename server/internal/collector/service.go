package collector

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ai-employee-platform/server/internal/secret"
	"github.com/robfig/cron/v3"
)

type Service struct {
	Store        Store
	Secrets      *secret.Manager
	HTTP         *http.Client
	FeishuURL    string
	OAuthURL     string
	AuthorizeURL string
	PlatformApp  func() (string, string, error)
	UserActive   func(context.Context, string) bool
	Audit        func(context.Context, string, string, string)
}

func NewService(st Store, secrets *secret.Manager) *Service {
	return &Service{Store: st, Secrets: secrets,
		HTTP:         safeHTTPClient(),
		FeishuURL:    "https://open.feishu.cn",
		OAuthURL:     "https://open.feishu.cn/open-apis/authen/v2/oauth/token",
		AuthorizeURL: "https://accounts.feishu.cn/open-apis/authen/v1/authorize"}
}

func randomID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func digest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (s *Service) active(ctx context.Context, owner string) error {
	if owner == "" || owner == "*" || s.UserActive != nil && !s.UserActive(ctx, owner) {
		return errors.New("用户不可用")
	}
	return nil
}
func (s *Service) lock(ctx context.Context, key string, ttl time.Duration) (func(), error) {
	holder := randomID()
	ok, err := s.Store.Acquire(ctx, key, holder, ttl)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrBusy
	}
	return func() { _ = s.Store.Release(context.Background(), key, holder) }, nil
}
func (s *Service) Profile(ctx context.Context, owner string) (Profile, error) {
	p, err := load[Profile](ctx, s.Store, owner, "profile", "self")
	if errors.Is(err, ErrNotFound) {
		return Profile{UserID: owner, Timezone: "Asia/Shanghai", Aliases: []string{}}, nil
	}
	return p, err
}
func (s *Service) credentials(ctx context.Context, p Profile) (Credentials, error) {
	if p.SecretRef == "" {
		return Credentials{}, nil
	}
	if s.Secrets == nil {
		return Credentials{}, errors.New("凭证保管服务不可用")
	}
	raw, err := s.Secrets.Access(ctx, p.SecretRef, "USER", p.UserID, "", "collector")
	if err != nil {
		return Credentials{}, err
	}
	var c Credentials
	err = json.Unmarshal([]byte(raw), &c)
	return c, err
}
func (s *Service) saveProfile(ctx context.Context, p Profile, c *Credentials) error {
	if c != nil {
		if s.Secrets == nil {
			return errors.New("凭证保管服务不可用")
		}
		b, _ := json.Marshal(c)
		// Stable reference + vault rotation avoids a new secret on every refresh.
		if p.SecretRef != "" {
			if _, err := s.Secrets.Rotate(ctx, p.SecretRef, string(b), p.UserID, ""); err != nil {
				return err
			}
		} else {
			ref, err := s.Secrets.Put(ctx, "collector:"+p.UserID, string(b), p.UserID, "", "工作采集个人凭证")
			if err != nil {
				return err
			}
			p.SecretRef = ref.ID
		}
	}
	p.UpdatedAt = time.Now().UTC()
	return s.Store.Commit(ctx, []Record{record(p.UserID, "profile", "self", p)})
}

func (s *Service) ProfileStatus(ctx context.Context, owner string) (map[string]any, error) {
	p, err := s.Profile(ctx, owner)
	if err != nil {
		return nil, err
	}
	c, err := s.credentials(ctx, p)
	if err != nil {
		return nil, err
	}
	p.SecretRef = ""
	p.GrantID = ""
	b, _ := json.Marshal(p)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	out["has_app_secret"] = c.AppSecret != ""
	out["has_jira_token"] = c.JiraToken != ""
	out["has_meego_token"] = c.MeegoToken != ""
	out["has_meego_secret"] = c.MeegoSecret != ""
	out["has_bot_webhook"] = c.BotWebhook != ""
	out["has_refresh_token"] = c.RefreshToken != ""
	out["has_webhook_secret"] = c.WebhookSecret != ""
	out["identity"] = "user"
	out["authorized"] = c.AccessToken != "" && p.FeishuOpenID != ""
	out["needs_reauthorization"] = c.AccessToken == "" || p.ExpiresAt == nil || time.Now().After(*p.ExpiresAt) && (c.RefreshToken == "" || p.RefreshExpiresAt != nil && time.Now().After(*p.RefreshExpiresAt))
	out["capabilities"] = capabilityStatus(p, c)
	if s.PlatformApp != nil {
		id, _, e := s.PlatformApp()
		out["platform_app_available"] = e == nil && id != ""
	}
	return out, nil
}

func (s *Service) UpdateProfile(ctx context.Context, owner string, in ProfileInput) error {
	if err := s.active(ctx, owner); err != nil {
		return err
	}
	unlock, err := s.lock(ctx, "identity:"+owner, time.Minute)
	if err != nil {
		return err
	}
	defer unlock()
	old, err := s.Profile(ctx, owner)
	if err != nil {
		return err
	}
	c, err := s.credentials(ctx, old)
	if err != nil {
		return err
	}
	p := in.Profile
	p.UserID, p.SecretRef = owner, old.SecretRef
	p.FeishuOpenID, p.FeishuName, p.ExpiresAt, p.RefreshExpiresAt = old.FeishuOpenID, old.FeishuName, old.ExpiresAt, old.RefreshExpiresAt
	p.GrantedScopes, p.GrantID = old.GrantedScopes, old.GrantID
	p.MeegoExpiresAt = old.MeegoExpiresAt
	if p.MeegoPluginID != old.MeegoPluginID {
		c.MeegoToken, c.MeegoSecret, p.MeegoExpiresAt = "", "", nil
	}
	if p.Timezone == "" {
		p.Timezone = "Asia/Shanghai"
	}
	if _, err := time.LoadLocation(p.Timezone); err != nil {
		return errors.New("无效时区")
	}
	if p.JiraURL != "" {
		if err := validateBaseURL(p.JiraURL); err != nil {
			return err
		}
	}
	if p.MeegoURL != "" {
		u, err := url.Parse(p.MeegoURL)
		if err != nil || u.Scheme != "https" || (u.Hostname() != "project.feishu.cn" && u.Hostname() != "project.larksuite.com") || u.User != nil || u.RawQuery != "" {
			return errors.New("飞书项目地址必须为 project.feishu.cn 或 project.larksuite.com")
		}
	}
	if p.JiraAuth != "" && p.JiraAuth != "basic" && p.JiraAuth != "pat" {
		return errors.New("Jira 认证方式应为 pat 或 basic")
	}
	if len(p.Aliases) > 50 || len(p.Scopes) > 8000 {
		return errors.New("配置过长")
	}
	// Changing the app invalidates the old user grant; never refresh under another app.
	if p.FeishuAppID != old.FeishuAppID {
		c.AppSecret, c.AccessToken, c.RefreshToken = "", "", ""
		p.FeishuOpenID, p.FeishuName, p.ExpiresAt, p.RefreshExpiresAt = "", "", nil, nil
		p.GrantedScopes, p.GrantID = "", randomID()
	}
	merge := func(dst *string, value string) {
		if value != "" {
			*dst = value
		}
	}
	merge(&c.AppSecret, in.Credentials.AppSecret)
	merge(&c.JiraToken, in.Credentials.JiraToken)
	merge(&c.MeegoToken, in.Credentials.MeegoToken)
	merge(&c.MeegoSecret, in.Credentials.MeegoSecret)
	if in.Credentials.MeegoToken != "" {
		p.MeegoExpiresAt = nil
	}
	merge(&c.BotWebhook, in.Credentials.BotWebhook)
	merge(&c.WebhookSecret, in.Credentials.WebhookSecret)
	if c.BotWebhook != "" {
		u, err := url.Parse(c.BotWebhook)
		if err != nil || u.Scheme != "https" || u.Host != "open.feishu.cn" || !strings.HasPrefix(u.Path, "/open-apis/bot/v2/hook/") || u.User != nil {
			return errors.New("无效飞书机器人 Webhook")
		}
	}
	for _, field := range in.ClearCredentials {
		switch field {
		case "jira_token":
			c.JiraToken = ""
		case "meego_token":
			c.MeegoToken = ""
			p.MeegoExpiresAt = nil
		case "meego_secret":
			c.MeegoSecret = ""
		case "bot_webhook":
			c.BotWebhook = ""
		case "webhook_secret":
			c.WebhookSecret = ""
		case "app_secret":
			c.AppSecret = ""
		case "feishu":
			c.AccessToken, c.RefreshToken = "", ""
			p.FeishuOpenID, p.FeishuName, p.ExpiresAt, p.RefreshExpiresAt = "", "", nil, nil
			p.GrantedScopes, p.GrantID = "", randomID()
		default:
			return errors.New("不支持清除的凭证字段")
		}
	}
	if in.Credentials.AccessToken != "" {
		if in.Profile.ExpiresAt == nil || !in.Profile.ExpiresAt.After(time.Now()) {
			return errors.New("导入用户凭证时需要未来的到期时间")
		}
		info, err := s.request(ctx, http.MethodGet, s.FeishuURL+"/open-apis/authen/v1/user_info", in.Credentials.AccessToken, nil, nil)
		if err != nil {
			return err
		}
		data := object(info["data"])
		if text(data, "open_id") == "" {
			return errors.New("凭证未返回用户身份，请使用 user_access_token")
		}
		c.AccessToken, c.RefreshToken = in.Credentials.AccessToken, in.Credentials.RefreshToken
		p.FeishuOpenID, p.FeishuName = text(data, "open_id"), text(data, "name")
		p.ExpiresAt, p.RefreshExpiresAt = in.Profile.ExpiresAt, in.Profile.RefreshExpiresAt
		// Manually imported scopes cannot be verified from user_info.
		p.GrantedScopes, p.GrantID = "", randomID()
	}
	return s.saveProfile(ctx, p, &c)
}

func (s *Service) SaveTarget(ctx context.Context, owner string, t Target) (Target, error) {
	if err := s.active(ctx, owner); err != nil {
		return t, err
	}
	if strings.TrimSpace(t.Name) == "" {
		return t, errors.New("请填写扫描目标名称")
	}
	switch t.Source {
	case "jira", "sheet", "wiki", "bitable", "meego", "feishu_im":
	default:
		return t, errors.New("不支持的来源")
	}
	if t.Config == nil {
		t.Config = map[string]any{}
	}
	if t.ID == "" {
		t.ID = randomID()
	}
	unlock, err := s.lock(ctx, "scan:"+owner+":"+t.ID, 6*time.Minute)
	if err != nil {
		return t, err
	}
	defer unlock()
	old, err := load[Target](ctx, s.Store, owner, "target", t.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return t, err
	}
	if err == nil && old.Version != t.Version {
		return t, errors.New("配置已更新，请刷新后重试")
	}
	if t.IntervalMinutes == 0 {
		t.IntervalMinutes = 60
	}
	if t.IntervalMinutes < 1 || t.IntervalMinutes > 10080 {
		return t, errors.New("扫描间隔应为 1–10080 分钟")
	}
	if t.Timezone == "" {
		t.Timezone = "Asia/Shanghai"
	}
	if _, err := time.LoadLocation(t.Timezone); err != nil {
		return t, errors.New("无效时区")
	}
	if t.Cron != "" {
		if _, err := cron.ParseStandard("CRON_TZ=" + t.Timezone + " " + t.Cron); err != nil {
			return t, errors.New("无效 Cron 表达式")
		}
	}
	if err := validateTarget(t); err != nil {
		return t, err
	}
	t.OwnerID, t.Version, t.UpdatedAt = owner, old.Version+1, time.Now().UTC()
	return t, s.Store.Commit(ctx, []Record{record(owner, "target", t.ID, t)})
}

func (s *Service) SaveRule(ctx context.Context, owner string, r Rule) (Rule, error) {
	if err := s.active(ctx, owner); err != nil {
		return r, err
	}
	if r.Name == "" {
		return r, errors.New("请填写规则名称")
	}
	if r.Action != "record" && r.Action != "instant" && r.Action != "batched" {
		return r, errors.New("无效通知方式")
	}
	if r.Match == "" {
		r.Match = "all"
	}
	if r.Match != "all" && r.Match != "any" {
		return r, errors.New("无效条件组合")
	}
	if len(r.Conditions) > 50 {
		return r, errors.New("条件最多 50 个")
	}
	for _, c := range r.Conditions {
		if c.Field == "" {
			return r, errors.New("条件缺少字段")
		}
		switch c.Operator {
		case "equals", "not_equals", "contains", "changed", "in", "is_me", "due_within", "exists":
		default:
			return r, errors.New("不支持的条件运算符")
		}
	}
	if r.AggregateMinutes < 1 {
		r.AggregateMinutes = 10
	}
	if r.AggregateMinutes > 1440 {
		return r, errors.New("合并窗口最多 1440 分钟")
	}
	if r.ID == "" {
		r.ID = randomID()
	}
	return r, s.Store.Commit(ctx, []Record{record(owner, "rule", r.ID, r)})
}

func (s *Service) List(ctx context.Context, owner, kind string) ([]Record, error) {
	switch kind {
	case "target", "rule", "run", "snapshot", "event", "delivery", "call":
	default:
		return nil, errors.New("无效记录类型")
	}
	return s.Store.List(ctx, owner, kind)
}
func (s *Service) Delete(ctx context.Context, owner, kind, id string) error {
	if kind != "target" && kind != "rule" {
		return errors.New("只允许删除目标或规则")
	}
	if kind == "target" {
		unlock, err := s.lock(ctx, "scan:"+owner+":"+id, 6*time.Minute)
		if err != nil {
			return err
		}
		defer unlock()
		if err := s.Store.Delete(ctx, owner, "snapshot", id); err != nil {
			return err
		}
		if err := s.Store.Delete(ctx, owner, "cursor", id); err != nil {
			return err
		}
		if err := s.Store.Delete(ctx, owner, "schedule", id); err != nil {
			return err
		}
		if err := s.Store.Delete(ctx, owner, "request", id); err != nil {
			return err
		}
	}
	return s.Store.Delete(ctx, owner, kind, id)
}
func (s *Service) Items(ctx context.Context, owner string) ([]Item, error) {
	snaps, err := list[Snapshot](ctx, s.Store, owner, "snapshot")
	if err != nil {
		return nil, err
	}
	out := []Item{}
	for _, snap := range snaps {
		out = append(out, snap.Items...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DueDate < out[j].DueDate })
	return out, nil
}

func text(m map[string]any, k string) string {
	if m == nil || m[k] == nil {
		return ""
	}
	if v, ok := m[k].(string); ok {
		return v
	}
	return fmt.Sprint(m[k])
}
func object(v any) map[string]any {
	if o, ok := v.(map[string]any); ok && o != nil {
		return o
	}
	return map[string]any{}
}
func array(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}
func integer(m map[string]any, k string, def int) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, e := v.Int64()
		if e == nil {
			return int(n)
		}
	}
	return def
}
func stringsOf(v any) []string {
	out := []string{}
	switch a := v.(type) {
	case []string:
		return a
	case string:
		for _, p := range strings.FieldsFunc(a, func(r rune) bool { return r == ',' || r == '，' || r == '\n' || r == ';' || r == '；' }) {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	case []any:
		for _, p := range a {
			if m, ok := p.(map[string]any); ok {
				for _, k := range []string{"id", "open_id", "name", "text"} {
					if str := text(m, k); str != "" {
						out = append(out, str)
					}
				}
			} else if p != nil {
				out = append(out, fmt.Sprint(p))
			}
		}
	}
	return out
}
func boolean(m map[string]any, k string, def bool) bool {
	if b, ok := m[k].(bool); ok {
		return b
	}
	return def
}

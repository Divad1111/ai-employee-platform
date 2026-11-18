package collector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Resolve and validate the actual dial address; validating only the URL allows
// DNS names to reach loopback/link-local services. Corporate private IPs remain
// supported for Jira installations.
func allowedAddress(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
}

func safeHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("站点地址解析失败")
		}
		for _, ip := range ips {
			if !allowedAddress(ip.IP) {
				return nil, errors.New("站点解析到受限地址")
			}
		}
		dialer := net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
		for _, ip := range ips {
			conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if e == nil {
				return conn, nil
			}
			err = e
		}
		return nil, err
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func validateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("站点地址必须是完整的 HTTP(S) 地址")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast()) {
		return errors.New("不可使用回环或元数据服务地址")
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return errors.New("不可使用 localhost 地址")
	}
	return nil
}

// Never follow redirects with a bearer token. Response bodies and OAuth payloads
// are deliberately excluded from error messages and audit records.
func (s *Service) request(ctx context.Context, method, endpoint, bearer string, headers map[string]string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, errors.New("无效上游地址")
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, errors.New("上游连接失败或请求超时")
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil || len(b) > 16<<20 {
		return nil, errors.New("上游响应过大或读取失败")
	}
	var out map[string]any
	if json.Unmarshal(b, &out) != nil {
		return nil, fmt.Errorf("上游返回了非 JSON 响应（HTTP %d）", resp.StatusCode)
	}
	code := integer(out, "code", 0)
	if code == 0 {
		code = integer(out, "err_code", 0)
	}
	if code == 0 {
		code = integer(out, "err", 0)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || code != 0 || out["error"] != nil {
		return nil, fmt.Errorf("上游请求失败（HTTP %d，code=%d）；请检查当前身份、授权范围和资源权限", resp.StatusCode, code)
	}
	return out, nil
}

func (s *Service) app(ctx context.Context, p Profile, c Credentials) (string, string, error) {
	if p.FeishuAppID != "" {
		if c.AppSecret == "" {
			return "", "", errors.New("请配置个人飞书应用 App Secret")
		}
		return p.FeishuAppID, c.AppSecret, nil
	}
	if s.PlatformApp != nil {
		return s.PlatformApp()
	}
	return "", "", errors.New("请配置个人飞书应用，或由管理员配置平台飞书应用")
}

type oauthState struct {
	Owner       string    `json:"owner"`
	BrowserHash string    `json:"browser_hash"`
	Redirect    string    `json:"redirect"`
	AppID       string    `json:"app_id"`
	Verifier    string    `json:"verifier"`
	Expires     time.Time `json:"expires"`
}

func (s *Service) BeginOAuth(ctx context.Context, owner, redirect, browser string) (string, error) {
	if err := s.active(ctx, owner); err != nil {
		return "", err
	}
	p, err := s.Profile(ctx, owner)
	if err != nil {
		return "", err
	}
	c, err := s.credentials(ctx, p)
	if err != nil {
		return "", err
	}
	appID, _, err := s.app(ctx, p, c)
	if err != nil {
		return "", err
	}
	if appID == "" {
		return "", errors.New("飞书应用未配置")
	}
	if strings.TrimSpace(p.Scopes) == "" {
		return "", errors.New("请选择本次需要授权的飞书权限范围")
	}
	state, verifier := randomID(), randomID()
	o := oauthState{owner, digest(browser), redirect, appID, verifier, time.Now().Add(10 * time.Minute)}
	if err := s.Store.Commit(ctx, []Record{record("_oauth", "state", digest(state), o)}); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {appID}, "redirect_uri": {redirect}, "response_type": {"code"}, "state": {state},
		"scope": {strings.Join(strings.Fields(p.Scopes), " ")}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}
	return s.AuthorizeURL + "?" + q.Encode(), nil
}

func (s *Service) CompleteOAuth(ctx context.Context, state, code, browser string) (string, error) {
	o, err := load[oauthState](ctx, s.Store, "_oauth", "state", digest(state))
	if err != nil || browser == "" || o.BrowserHash != digest(browser) || time.Now().After(o.Expires) {
		return "", errors.New("授权会话无效或已过期，请重新发起")
	}
	if err := s.active(ctx, o.Owner); err != nil {
		return "", err
	}
	unlock, err := s.lock(ctx, "identity:"+o.Owner, time.Minute)
	if err != nil {
		return "", err
	}
	defer unlock()
	if _, err = s.Store.Take(ctx, "_oauth", "state", digest(state)); err != nil {
		return "", errors.New("授权回调已使用")
	}
	p, err := s.Profile(ctx, o.Owner)
	if err != nil {
		return "", err
	}
	c, err := s.credentials(ctx, p)
	if err != nil {
		return "", err
	}
	id, appSecret, err := s.app(ctx, p, c)
	if err != nil {
		return "", err
	}
	if id != o.AppID {
		return "", errors.New("应用配置已改变，请重新授权")
	}
	data, err := s.request(ctx, "POST", s.OAuthURL, "", nil, map[string]any{
		"grant_type": "authorization_code", "client_id": id, "client_secret": appSecret,
		"code": code, "redirect_uri": o.Redirect, "code_verifier": o.Verifier})
	if err != nil {
		return "", err
	}
	if err = s.acceptToken(ctx, &p, &c, data); err != nil {
		return "", err
	}
	return o.Owner, s.saveProfile(ctx, p, &c)
}

func (s *Service) acceptToken(ctx context.Context, p *Profile, c *Credentials, data map[string]any) error {
	refreshing := text(data, "grant_type") == "refresh_token"
	if data["data"] != nil {
		data = object(data["data"])
	}
	token := text(data, "access_token")
	if token == "" {
		return errors.New("飞书未返回用户凭证")
	}
	info, err := s.request(ctx, "GET", s.FeishuURL+"/open-apis/authen/v1/user_info", token, nil, nil)
	if err != nil {
		return err
	}
	user := object(info["data"])
	if text(user, "open_id") == "" {
		return errors.New("飞书未返回用户身份")
	}
	if refreshing && p.FeishuOpenID != "" && text(user, "open_id") != p.FeishuOpenID {
		return errors.New("续期返回了不同的用户")
	}
	if !refreshing {
		c.RefreshToken = ""
		p.RefreshExpiresAt, p.GrantedScopes, p.GrantID = nil, "", randomID()
	}
	p.FeishuOpenID, p.FeishuName = text(user, "open_id"), text(user, "name")
	exp := time.Now().UTC().Add(time.Duration(integer(data, "expires_in", 7200)) * time.Second)
	p.ExpiresAt = &exp
	c.AccessToken = token
	if refresh := text(data, "refresh_token"); refresh != "" {
		c.RefreshToken = refresh
	}
	if seconds := integer(data, "refresh_token_expires_in", 0); seconds > 0 {
		exp := time.Now().UTC().Add(time.Duration(seconds) * time.Second)
		p.RefreshExpiresAt = &exp
	}
	if scopes := text(data, "scope"); scopes != "" {
		p.GrantedScopes = scopes
	}
	return nil
}

func (s *Service) accessToken(ctx context.Context, owner, identity string) (string, error) {
	if err := s.active(ctx, owner); err != nil {
		return "", err
	}
	p, err := s.Profile(ctx, owner)
	if err != nil {
		return "", err
	}
	c, err := s.credentials(ctx, p)
	if err != nil {
		return "", err
	}
	if identity == "app" {
		if p.FeishuAppID == "" {
			return "", errors.New("应用身份需要配置本人管理的飞书应用；平台应用仅用于个人 OAuth 授权")
		}
		id, appSecret, err := s.app(ctx, p, c)
		if err != nil {
			return "", err
		}
		data, err := s.request(ctx, "POST", s.FeishuURL+"/open-apis/auth/v3/tenant_access_token/internal", "", nil, map[string]any{"app_id": id, "app_secret": appSecret})
		if err != nil {
			return "", err
		}
		if token := text(data, "tenant_access_token"); token != "" {
			return token, nil
		}
		return "", errors.New("应用凭证不可用")
	}
	if identity != "" && identity != "user" {
		return "", errors.New("identity 只能为 user 或 app")
	}
	if c.AccessToken == "" || p.FeishuOpenID == "" {
		return "", errors.New("请先在个人设置中授权飞书用户身份")
	}
	if p.ExpiresAt != nil && time.Until(*p.ExpiresAt) > 2*time.Minute {
		return c.AccessToken, nil
	}
	unlock, err := s.lock(ctx, "identity:"+owner, time.Minute)
	if err != nil {
		return "", err
	}
	defer unlock()
	p, err = s.Profile(ctx, owner)
	if err != nil {
		return "", err
	}
	c, err = s.credentials(ctx, p)
	if err != nil {
		return "", err
	}
	if c.AccessToken == "" {
		return "", errors.New("飞书授权已断开")
	}
	if p.ExpiresAt != nil && time.Until(*p.ExpiresAt) > 2*time.Minute {
		return c.AccessToken, nil
	}
	if c.RefreshToken == "" || p.RefreshExpiresAt != nil && time.Now().After(*p.RefreshExpiresAt) {
		return "", errors.New("飞书个人授权已过期，请重新授权")
	}
	id, appSecret, err := s.app(ctx, p, c)
	if err != nil {
		return "", err
	}
	data, err := s.request(ctx, "POST", s.OAuthURL, "", nil, map[string]any{"grant_type": "refresh_token", "client_id": id, "client_secret": appSecret, "refresh_token": c.RefreshToken})
	if err != nil {
		return "", errors.New("飞书个人授权续期失败，请重新授权")
	}
	data["grant_type"] = "refresh_token"
	if err = s.acceptToken(ctx, &p, &c, data); err != nil {
		return "", err
	}
	if err = s.saveProfile(ctx, p, &c); err != nil {
		return "", err
	}
	return c.AccessToken, nil
}

func (s *Service) Feishu(ctx context.Context, owner, identity, method, path string, q url.Values, body any) (map[string]any, error) {
	if !strings.HasPrefix(path, "/open-apis/") || strings.Contains(path, "..") || strings.ContainsAny(path, "?#\\") {
		return nil, errors.New("无效飞书资源路径")
	}
	token, err := s.accessToken(ctx, owner, identity)
	if err != nil {
		return nil, err
	}
	endpoint := s.FeishuURL + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	out, err := s.request(ctx, method, endpoint, token, nil, body)
	if err != nil {
		return nil, err
	}
	return object(out["data"]), nil
}

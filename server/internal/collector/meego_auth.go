package collector

import (
	"context"
	"errors"
	"strings"
	"time"
)

func (s *Service) meegoToken(ctx context.Context, owner string) (string, error) {
	unlock, err := s.lock(ctx, "identity:"+owner, time.Minute)
	if err != nil {
		return "", err
	}
	defer unlock()
	p, err := s.Profile(ctx, owner)
	if err != nil {
		return "", err
	}
	c, err := s.credentials(ctx, p)
	if err != nil {
		return "", err
	}
	if c.MeegoToken != "" && (p.MeegoExpiresAt == nil || time.Until(*p.MeegoExpiresAt) > time.Minute) {
		return c.MeegoToken, nil
	}
	if p.MeegoPluginID == "" || c.MeegoSecret == "" {
		return "", errors.New("请配置飞书项目 Plugin ID / Secret，或导入有效的 Plugin Token")
	}
	base := strings.TrimRight(valueOr(p.MeegoURL, "https://project.feishu.cn"), "/")
	out, err := s.request(ctx, "POST", base+"/open_api/authen/plugin_token", "", nil, map[string]any{
		"plugin_id": p.MeegoPluginID, "plugin_secret": c.MeegoSecret, "type": 0,
	})
	if err != nil {
		return "", err
	}
	data := object(out["data"])
	if text(data, "token") == "" {
		return "", errors.New("飞书项目未返回插件凭证")
	}
	expires := time.Unix(int64(integer(data, "expire_time", int(time.Now().Add(time.Hour).Unix()))), 0)
	c.MeegoToken, p.MeegoExpiresAt = text(data, "token"), &expires
	if err := s.saveProfile(ctx, p, &c); err != nil {
		return "", err
	}
	return c.MeegoToken, nil
}

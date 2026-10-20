package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var (
	ErrCredentialInactive      = errors.New("凭证已停用或吊销")
	ErrCredentialExpired       = errors.New("凭证已过期，请更新凭证；暂不支持自动续期")
	ErrCredentialSecretMissing = errors.New("凭证密钥不可用")
)

// Authentication 仅供运行时下发使用，不得输出到普通日志或管理 API。
type Authentication struct {
	Headers map[string]string `json:"-"`
	Env     map[string]string `json:"-"`
}

// ResolveAuthentication 根据凭证类型和 metadata 生成 HTTP 头或 STDIO 环境变量。
// 旧凭证未配置 metadata 时保留 Bearer 行为；Token、Basic 和自定义头显式适配。
func (s *Service) ResolveAuthentication(ctx context.Context, credentialID, transport string) (*Authentication, error) {
	cred, err := s.usableCredential(ctx, credentialID)
	if err != nil {
		return nil, err
	}
	if err := validateAuthConfig(cred.AuthType, cred.Metadata); err != nil {
		return nil, err
	}
	value, err := s.credentialSecret(ctx, cred)
	if err != nil {
		return nil, err
	}
	result := &Authentication{Headers: map[string]string{}, Env: map[string]string{}}
	envName, _, _ := authOption(cred.Metadata, "env_name")
	if transport == TransportStdio {
		if envName == "" {
			return nil, errors.New("STDIO 凭证必须配置 env_name")
		}
		result.Env[envName] = value
		return result, nil
	}
	if envName != "" {
		return nil, errors.New("env_name 仅适用于 STDIO MCP")
	}
	header, explicitHeader, _ := authOption(cred.Metadata, "header_name")
	if !explicitHeader {
		header = "Authorization"
	}
	prefix, explicitPrefix, _ := authOption(cred.Metadata, "header_prefix")
	if !explicitPrefix {
		switch cred.AuthType {
		case AuthTypeBasic:
			prefix = "Basic"
		case AuthTypeToken:
			prefix = "Token"
		default:
			prefix = "Bearer"
			if explicitHeader && !strings.EqualFold(header, "Authorization") {
				prefix = ""
			}
		}
	}
	if cred.AuthType == AuthTypeBasic {
		if !strings.Contains(value, ":") {
			return nil, errors.New("Basic 凭证密钥必须使用 用户名:密码 格式")
		}
		value = base64.StdEncoding.EncodeToString([]byte(value))
	}
	if prefix != "" {
		value = prefix + " " + value
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return nil, errors.New("凭证不能包含非法请求头字符")
	}
	result.Headers[http.CanonicalHeaderKey(header)] = value
	return result, nil
}

func (s *Service) usableCredential(ctx context.Context, id string) (*Credential, error) {
	cred, err := s.store.GetCredential(ctx, id)
	if err != nil {
		return nil, err
	}
	if cred.Status == StatusExpired {
		return nil, ErrCredentialExpired
	}
	if cred.Status != StatusActive {
		return nil, ErrCredentialInactive
	}
	if cred.ExpiresAt != nil && !time.Now().Before(*cred.ExpiresAt) {
		return nil, ErrCredentialExpired
	}
	return cred, nil
}

func (s *Service) credentialSecret(ctx context.Context, cred *Credential) (string, error) {
	if cred.SecretRef == "" || s.secretMgr == nil {
		return "", ErrCredentialSecretMissing
	}
	value, err := s.secretMgr.Access(ctx, cred.SecretRef, "SYSTEM", "scheduler", "", "mcp_dispatch")
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", ErrCredentialSecretMissing
	}
	return value, nil
}

// authOption 只接受非敏感的字符串配置，避免把 JSON 类型错误静默解释为默认认证。
func authOption(metadata map[string]any, key string) (string, bool, error) {
	v, exists := metadata[key]
	if !exists {
		return "", false, nil
	}
	value, ok := v.(string)
	if !ok {
		return "", true, fmt.Errorf("%s 必须是字符串", key)
	}
	return strings.TrimSpace(value), true, nil
}

func validateAuthConfig(authType string, metadata map[string]any) error {
	switch authType {
	case AuthTypeOAuth, AuthTypeAPIKey, AuthTypeBearer, AuthTypeToken, AuthTypePAT, AuthTypeBasic:
	default:
		return errors.New("不支持的凭证认证类型")
	}
	for _, key := range []string{"header_name", "header_prefix", "env_name"} {
		value, exists, err := authOption(metadata, key)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if key == "header_prefix" && value == "" {
			continue
		}
		if value == "" {
			return fmt.Errorf("%s 不能为空", key)
		}
		if key == "env_name" {
			for i, ch := range value {
				if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch == '_' || i > 0 && ch >= '0' && ch <= '9') {
					return errors.New("env_name 必须是合法的环境变量名")
				}
			}
			continue
		}
		for _, ch := range value {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", ch)) {
				return fmt.Errorf("%s 包含非法字符", key)
			}
		}
	}
	if _, hasEnv := metadata["env_name"]; hasEnv {
		if _, hasHeader := metadata["header_name"]; hasHeader {
			return errors.New("环境变量注入不能同时配置请求头")
		}
		if _, hasPrefix := metadata["header_prefix"]; hasPrefix {
			return errors.New("环境变量注入不能同时配置认证前缀")
		}
	}
	return nil
}

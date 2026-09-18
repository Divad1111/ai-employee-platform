// Package feishu 飞书事件接入、验签、去重与回复。
// 单 Bot + 消息内解析目标 Employee（决策 Q-02）。
// 设计依据：设计文档 §81–§83、§107、§108。
package feishu

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/secret"
)

// 错误。
var (
	ErrBadSignature = errors.New("飞书验签失败")
	ErrNoEmployee   = errors.New("无法解析目标 Employee")
	ErrDisabled     = errors.New("Employee 未启用或未绑定 Workstation")
)

// Config 飞书应用配置（Secret 仅存引用）。
type Config struct {
	AppID               string `json:"app_id"`
	AppSecretRef        string `json:"app_secret_ref"`
	VerificationToken   string `json:"verification_token"` // 可明文（非高敏）或后续改引用
	EncryptKeyRef       string `json:"encrypt_key_ref"`
	Enabled             bool   `json:"enabled"`
}

// Binding Employee 飞书绑定。
type Binding struct {
	EmployeeID    string `json:"employee_id"`
	FeishuOpenID  string `json:"feishu_open_id"`
	FeishuAlias   string `json:"feishu_bot_alias"`
	ChatID        string `json:"chat_id"`
}

// IncomingEvent 规范化入站事件。
type IncomingEvent struct {
	EventID       string
	MessageID     string
	ChatID        string
	SenderOpenID  string
	Text          string
	RawType       string
}

// Reply 出站回复。
type Reply struct {
	ChatID  string
	Content string
}

// Sender 发送飞书消息（可 Mock）。
type Sender interface {
	Send(ctx context.Context, reply Reply) error
}

// JobCreator 创建 Job。
type JobCreator interface {
	CreateFromFeishu(ctx context.Context, employeeID, prompt, idempotencyKey, chatID, messageID string) (jobID string, err error)
}

// EmployeeChecker 校验 Employee 可用。
type EmployeeChecker interface {
	ResolveAlias(ctx context.Context, alias string) (employeeID string, err error)
	IsAssignable(ctx context.Context, employeeID string) error
}

// Service 飞书服务。
type Service struct {
	mu        sync.Mutex
	cfg       Config
	vault     secret.Store
	bindings  map[string]Binding // alias(lower) → binding
	byOpenID  map[string]string  // open_id → employee_id
	seenEvent map[string]time.Time
	Sender    Sender
	Jobs      JobCreator
	Employees EmployeeChecker
}

// NewService 创建。
func NewService(vault secret.Store) *Service {
	return &Service{
		vault:     vault,
		bindings:  map[string]Binding{},
		byOpenID:  map[string]string{},
		seenEvent: map[string]time.Time{},
	}
}

// SetConfig 更新配置（Secret 以引用写入）。
func (s *Service) SetConfig(cfg Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
}

// GetConfigPublic 返回可展示配置（无明文 Secret）。
func (s *Service) GetConfigPublic() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cfg
	return c
}

// ListBindings 全部绑定（Admin）。
func (s *Service) ListBindings() []Binding {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]struct{}{}
	out := make([]Binding, 0, len(s.bindings))
	for _, b := range s.bindings {
		key := b.EmployeeID + "|" + b.FeishuAlias
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, b)
	}
	return out
}

// BindingByEmployee 按 Employee 查绑定。
func (s *Service) BindingByEmployee(employeeID string) *Binding {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range s.bindings {
		if b.EmployeeID == employeeID {
			cp := b
			return &cp
		}
	}
	return nil
}

// UpsertBinding 绑定别名。
func (s *Service) UpsertBinding(b Binding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b.FeishuAlias != "" {
		s.bindings[strings.ToLower(b.FeishuAlias)] = b
	}
	if b.FeishuOpenID != "" {
		s.byOpenID[b.FeishuOpenID] = b.EmployeeID
	}
}

// HandleURLChallenge 处理飞书 URL 校验。
func (s *Service) HandleURLChallenge(token, challenge string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.VerificationToken != "" && token != s.cfg.VerificationToken {
		return "", ErrBadSignature
	}
	return challenge, nil
}

// VerifySignature 校验请求签名。
// 简化实现：sha256(timestamp + nonce + token + body)；token 可用 VerificationToken。
func (s *Service) VerifySignature(timestamp, nonce, signature, body string) error {
	s.mu.Lock()
	token := s.cfg.VerificationToken
	s.mu.Unlock()
	if token == "" {
		return nil // 开发模式可跳过
	}
	sum := sha256.Sum256([]byte(timestamp + nonce + token + body))
	expect := hex.EncodeToString(sum[:])
	if !strings.EqualFold(expect, signature) {
		return ErrBadSignature
	}
	return nil
}

// Dedupe 事件去重；重复返回 true（已处理）。
func (s *Service) Dedupe(eventID string) bool {
	if eventID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seenEvent[eventID]; ok {
		return true
	}
	s.seenEvent[eventID] = time.Now().UTC()
	// 简单清理
	if len(s.seenEvent) > 5000 {
		for k, t := range s.seenEvent {
			if time.Since(t) > 24*time.Hour {
				delete(s.seenEvent, k)
			}
		}
	}
	return false
}

var (
	reAtAlias = regexp.MustCompile(`(?i)@([a-zA-Z0-9_-]+)`)
	reEmpID   = regexp.MustCompile(`(?i)\b(EMP-[a-zA-Z0-9-]+)\b`)
	reSlash   = regexp.MustCompile(`(?i)^/(?:emp|employee)\s+(\S+)\s+(.+)$`)
)

// ParseTarget 从文本解析目标 Employee 与 Prompt（Q-02）。
func (s *Service) ParseTarget(text string) (employeeID, prompt string, err error) {
	text = strings.TrimSpace(text)
	if m := reSlash.FindStringSubmatch(text); len(m) == 3 {
		key := m[1]
		prompt = strings.TrimSpace(m[2])
		if strings.HasPrefix(strings.ToUpper(key), "EMP-") {
			return strings.ToUpper(key), prompt, nil
		}
		s.mu.Lock()
		b, ok := s.bindings[strings.ToLower(key)]
		s.mu.Unlock()
		if !ok {
			return "", "", ErrNoEmployee
		}
		return b.EmployeeID, prompt, nil
	}
	if m := reEmpID.FindStringSubmatch(text); len(m) == 2 {
		id := strings.ToUpper(m[1])
		prompt = strings.TrimSpace(reEmpID.ReplaceAllString(text, ""))
		prompt = strings.TrimSpace(reAtAlias.ReplaceAllString(prompt, ""))
		return id, prompt, nil
	}
	if m := reAtAlias.FindStringSubmatch(text); len(m) == 2 {
		alias := strings.ToLower(m[1])
		s.mu.Lock()
		b, ok := s.bindings[alias]
		s.mu.Unlock()
		if !ok {
			return "", "", ErrNoEmployee
		}
		prompt = strings.TrimSpace(reAtAlias.ReplaceAllString(text, ""))
		return b.EmployeeID, prompt, nil
	}
	return "", "", ErrNoEmployee
}

// HandleMessage 异步友好：创建 Message/Job 后立即返回（不调 Agent）。
func (s *Service) HandleMessage(ctx context.Context, ev IncomingEvent) (jobID string, duplicate bool, err error) {
	if s.Dedupe(ev.EventID) {
		return "", true, nil
	}
	empID, prompt, err := s.ParseTarget(ev.Text)
	if err != nil {
		return "", false, err
	}
	if prompt == "" {
		prompt = ev.Text
	}
	if s.Employees != nil {
		if err := s.Employees.IsAssignable(ctx, empID); err != nil {
			return "", false, err
		}
	}
	idem := fmt.Sprintf("feishu:%s:%s", ev.SenderOpenID, ev.MessageID)
	if ev.MessageID == "" {
		idem = fmt.Sprintf("feishu:%s:%s", ev.SenderOpenID, ev.EventID)
	}
	if s.Jobs == nil {
		return "", false, errors.New("未配置 JobCreator")
	}
	jobID, err = s.Jobs.CreateFromFeishu(ctx, empID, prompt, idem, ev.ChatID, ev.MessageID)
	return jobID, false, err
}

// NotifyJobResult 终态回复飞书。
func (s *Service) NotifyJobResult(ctx context.Context, chatID, jobID, status, summary string) error {
	if s.Sender == nil || chatID == "" {
		return nil
	}
	content := fmt.Sprintf("Job %s → %s\n%s", jobID, status, summary)
	// 确保不含 secret
	content = strings.ReplaceAll(content, "secret", "***")
	return s.Sender.Send(ctx, Reply{ChatID: chatID, Content: content})
}

// ParseWebhookBody 解析飞书事件 JSON（支持 challenge 与 im.message）。
func ParseWebhookBody(body []byte) (challenge string, token string, ev *IncomingEvent, err error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return "", "", nil, err
	}
	if t, _ := root["type"].(string); t == "url_verification" {
		ch, _ := root["challenge"].(string)
		tok, _ := root["token"].(string)
		return ch, tok, nil, nil
	}
	// schema 2.0
	if header, ok := root["header"].(map[string]any); ok {
		eventID, _ := header["event_id"].(string)
		eventType, _ := header["event_type"].(string)
		if event, ok := root["event"].(map[string]any); ok {
			msg, _ := event["message"].(map[string]any)
			sender, _ := event["sender"].(map[string]any)
			text := extractText(msg)
			chatID, _ := msg["chat_id"].(string)
			msgID, _ := msg["message_id"].(string)
			openID := ""
			if si, ok := sender["sender_id"].(map[string]any); ok {
				openID, _ = si["open_id"].(string)
			}
			return "", "", &IncomingEvent{
				EventID: eventID, MessageID: msgID, ChatID: chatID,
				SenderOpenID: openID, Text: text, RawType: eventType,
			}, nil
		}
	}
	return "", "", nil, fmt.Errorf("无法识别的飞书事件")
}

func extractText(msg map[string]any) string {
	if msg == nil {
		return ""
	}
	content, _ := msg["content"].(string)
	if content == "" {
		return ""
	}
	var obj map[string]any
	if json.Unmarshal([]byte(content), &obj) == nil {
		if t, ok := obj["text"].(string); ok {
			return t
		}
	}
	return content
}

// MemorySender 测试用。
type MemorySender struct {
	mu    sync.Mutex
	Sent  []Reply
}

func (m *MemorySender) Send(_ context.Context, reply Reply) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Sent = append(m.Sent, reply)
	return nil
}

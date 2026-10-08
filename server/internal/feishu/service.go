// Package feishu: 飞书开放平台集成服务（统一基于官方 Go SDK）。
// 支持 WebSocket 长连接网关（免公网IP）与 HTTP Webhook 双模事件接入，
// 统一出站任务回执与连通性自检。
// 设计依据：设计文档 §81–§83、§107、§108。
package feishu

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ai-employee-platform/server/internal/secret"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/core/httpserverext"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkcontact "github.com/larksuite/oapi-sdk-go/v3/service/contact/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// 错误定义
var (
	ErrBadSignature = errors.New("飞书验签失败")
	ErrNoEmployee   = errors.New("无法解析目标 Employee")
	ErrDisabled     = errors.New("Employee 未启用或未绑定 Workstation")
)

// Config 飞书应用配置（Secret 仅存引用）
type Config struct {
	AppID             string `json:"app_id"`
	AppSecretRef      string `json:"app_secret_ref"`
	VerificationToken string `json:"verification_token"`
	EncryptKeyRef     string `json:"encrypt_key_ref"`
	Enabled           bool   `json:"enabled"`
}

// Binding Employee 飞书绑定
type Binding struct {
	EmployeeID   string `json:"employee_id"`
	FeishuOpenID string `json:"feishu_open_id"`
	FeishuAlias  string `json:"feishu_bot_alias"`
	ChatID       string `json:"chat_id"`
}

// IncomingEvent 规范化入站事件
type IncomingEvent struct {
	EventID       string
	MessageID     string
	ParentID      string
	ChatID        string
	ChatType      string // 飞书 chat_type：p2p=私聊，group=群聊
	SenderOpenID  string
	Text          string
	QuotedContent string
	RawType       string
}

// Reply 出站回复
type Reply struct {
	ChatID  string
	Content string
}

// SendResult 消息发送返回结果
type SendResult struct {
	MessageID string    `json:"message_id"`
	ChatID    string    `json:"chat_id"`
	SentAt    time.Time `json:"sent_at"`
}

// StatusResult 飞书集成整体运行状态
type StatusResult struct {
	Configured     bool      `json:"configured"`
	Enabled        bool      `json:"enabled"`
	Connected      bool      `json:"connected"`
	AppID          string    `json:"app_id,omitempty"`
	BotName        string    `json:"bot_name,omitempty"`
	BotOpenID      string    `json:"bot_open_id,omitempty"`
	ActivateStatus int       `json:"activate_status,omitempty"`
	LatencyMs      int64     `json:"latency_ms,omitempty"`
	Error          string    `json:"error,omitempty"`
	WSState        string    `json:"ws_state,omitempty"` // DISCONNECTED / CONNECTING / CONNECTED / ERROR
	WSError        string    `json:"ws_error,omitempty"`
	LastCheckedAt  time.Time `json:"last_checked_at"`
}

// Sender 发送飞书消息（可 Mock）
type Sender interface {
	Send(ctx context.Context, reply Reply) error
}

// JobCreator 创建 Job
type JobCreator interface {
	CreateFromFeishu(ctx context.Context, employeeID, prompt, idempotencyKey, chatID, messageID, senderOpenID string) (jobID string, err error)
}

// EmployeeChecker 校验与查询 Employee
type EmployeeChecker interface {
	ResolveAlias(ctx context.Context, alias string) (employeeID string, err error)
	IsAssignable(ctx context.Context, employeeID string) error
	GetEmployeeName(ctx context.Context, employeeID string) string
}

// Service 飞书统一集成服务
type Service struct {
	mu        sync.RWMutex
	cfg       Config
	vault     secret.Store
	bindings  map[string]Binding  // alias(lower) → binding
	byOpenID  map[string][]string // open_id → 可绑定多个数字员工
	seenEvent map[string]time.Time
	Sender    Sender
	Jobs      JobCreator
	Employees EmployeeChecker

	// 官方 SDK 核心驱动组件
	larkClient     *lark.Client
	dispatcher     *dispatcher.EventDispatcher
	webhookHandler http.HandlerFunc
	Gateway        *WSGateway

	cachedStatus    *StatusResult
	lastStatusCheck time.Time

	userNameCache sync.Map // open_id (string) -> user_name (string)
	Inquiry       InquiryHandler
}

// InquiryHandler 外部决策请示交互门面
type InquiryHandler interface {
	HandleCardAction(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error)
	CheckChatReply(ctx context.Context, chatID, text, senderOpenID string) (bool, error)
}

// SetInquiryHandler 设置询问决策处理器
func (s *Service) SetInquiryHandler(h InquiryHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Inquiry = h
}

// NewService 创建飞书集成服务
func NewService(vault secret.Store) *Service {
	svc := &Service{
		vault:     vault,
		bindings:  map[string]Binding{},
		byOpenID:  map[string][]string{},
		seenEvent: map[string]time.Time{},
		Gateway:   NewWSGateway(),
	}
	svc.Sender = NewSDKSender(svc)
	svc.loadPersistedLocked()
	return svc
}

// loadPersistedLocked 从持久化 Vault 中恢复已保存的飞书配置与别名绑定
func (s *Service) loadPersistedLocked() {
	if s.vault == nil {
		return
	}
	for _, ref := range s.vault.List() {
		if ref.Name == "feishu.config_data" {
			if str, err := s.vault.Get(ref.ID); err == nil && str != "" {
				var cfg Config
				if json.Unmarshal([]byte(str), &cfg) == nil && cfg.AppID != "" {
					s.cfg = cfg
					s.rebuildLarkLocked()
				}
			}
		}
		if ref.Name == "feishu.bindings_data" {
			if str, err := s.vault.Get(ref.ID); err == nil && str != "" {
				var list []Binding
				if json.Unmarshal([]byte(str), &list) == nil {
					for _, b := range list {
						if b.FeishuAlias != "" {
							s.bindings[aliasKey(b.FeishuAlias)] = b
						}
						if b.FeishuOpenID != "" {
							s.indexOpenIDLocked(b.FeishuOpenID, b.EmployeeID)
						}
					}
				}
			}
		}
	}
}

// SetLarkClient 设置官方 SDK Client（供测试 Mock 注入）
func (s *Service) SetLarkClient(c *lark.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.larkClient = c
}

// SetConfig 更新配置并重新加载官方 SDK 驱动
func (s *Service) SetConfig(cfg Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
	s.cachedStatus = nil
	if s.vault != nil {
		if rot, ok := s.vault.(secret.Rotator); ok {
			for _, ref := range s.vault.List() {
				if ref.Name == "feishu.config_data" {
					_ = rot.Delete(ref.ID)
				}
			}
		}
		if b, err := json.Marshal(cfg); err == nil {
			_, _ = s.vault.Put("feishu.config_data", string(b))
		}
	}
	s.rebuildLarkLocked()
}

// GetConfigPublic 返回可公开展示的配置
func (s *Service) GetConfigPublic() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// WebhookHandler 返回基于官方 SDK 处理 Webhook 的 HTTP Handler
func (s *Service) WebhookHandler() http.HandlerFunc {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.webhookHandler
}

// GetAppSecret 从 Vault 读取明文 App Secret
func (s *Service) GetAppSecret() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getAppSecretLocked()
}

func (s *Service) getAppSecretLocked() (string, error) {
	refID := s.cfg.AppSecretRef
	if s.vault == nil || refID == "" {
		return "", fmt.Errorf("app_secret 未配置")
	}
	return s.vault.Get(refID)
}

// StartGateway 启动或重新初始化长连接网关
func (s *Service) StartGateway() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rebuildLarkLocked()
	return nil
}

// rebuildLarkLocked 重新初始化官方 Client 与共享 EventDispatcher
func (s *Service) rebuildLarkLocked() {
	if s.Gateway != nil {
		s.Gateway.Stop()
	}
	s.larkClient = nil
	s.dispatcher = nil
	s.webhookHandler = nil

	if !s.cfg.Enabled || s.cfg.AppID == "" || s.cfg.AppSecretRef == "" {
		return
	}

	secretVal, err := s.getAppSecretLocked()
	if err != nil || secretVal == "" {
		return
	}

	encryptKey := ""
	if s.cfg.EncryptKeyRef != "" && s.vault != nil {
		encryptKey, _ = s.vault.Get(s.cfg.EncryptKeyRef)
	}

	// 1. 创建官方 Client
	s.larkClient = lark.NewClient(s.cfg.AppID, secretVal, lark.WithLogLevel(larkcore.LogLevelInfo))

	// 2. 创建官方 EventDispatcher，同时作为长连接和 Webhook 的共享事件中心
	disp := dispatcher.NewEventDispatcher(s.cfg.VerificationToken, encryptKey).
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			if event == nil || event.Event == nil || event.Event.Message == nil {
				return nil
			}
			msg := event.Event.Message
			rawText := ""
			if msg.Content != nil {
				rawText = extractTextContent(*msg.Content)
			}
			openID := ""
			if event.Event.Sender != nil && event.Event.Sender.SenderId != nil && event.Event.Sender.SenderId.OpenId != nil {
				openID = *event.Event.Sender.SenderId.OpenId
			}
			chatID := ""
			if msg.ChatId != nil {
				chatID = *msg.ChatId
			}
			chatType := ""
			if msg.ChatType != nil {
				chatType = *msg.ChatType
			}
			msgID := ""
			if msg.MessageId != nil {
				msgID = *msg.MessageId
			}
			parentID := ""
			if msg.ParentId != nil {
				parentID = *msg.ParentId
			}
			eventID := ""
			if event.EventV2Base != nil && event.EventV2Base.Header != nil {
				eventID = event.EventV2Base.Header.EventID
			}

			// 1. 处理 @mentions：将 @_user_x 占位符替换为具体别名或用户姓名
			text := s.ResolveMentions(rawText, msg.Mentions)
			text = appendMentionNames(text, msg.Mentions)

			// 2. 如果存在引用/回复父消息，拉取父消息内容作为上下文
			quotedContent := ""
			if parentID != "" {
				if qc, err := s.FetchMessageContent(ctx, parentID); err == nil && qc != "" {
					quotedContent = qc
					fmt.Printf("[Feishu] 📎 提取到引用/回复父消息上下文 (parentID=%s): %s\n", parentID, truncateStr(qc, 100))
				} else if err != nil {
					fmt.Printf("[Feishu] ⚠️ 获取引用父消息失败 (parentID=%s): %v\n", parentID, err)
				}
			}

			incoming := IncomingEvent{
				EventID:       eventID,
				MessageID:     msgID,
				ParentID:      parentID,
				ChatID:        chatID,
				ChatType:      chatType,
				SenderOpenID:  openID,
				Text:          text,
				QuotedContent: quotedContent,
				RawType:       "im.message.receive_v1",
			}
			fmt.Printf("[Feishu] 📩 收到消息事件: sender=%s, chat=%s, chatType=%s, msgID=%s, text=%q codes=%s parentID=%s\n", openID, chatID, chatType, msgID, text, runeCodes(text), parentID)
			jobID, dup, err := s.HandleMessage(ctx, incoming)
			if err != nil {
				fmt.Printf("[Feishu] ⚠️ 消息处理反馈: %v (text=%q)\n", err, text)
			} else if dup {
				fmt.Printf("[Feishu] ⏩ 忽略重复事件: %s\n", eventID)
			} else {
				fmt.Printf("[Feishu] ✅ 成功派发协同任务: JobID=%s (chat=%s)\n", jobID, chatID)
			}
			return nil
		}).
		// 处理首次单聊会话创建事件（用户首次向机器人发起私聊）
		OnP1P2PChatCreatedV1(func(ctx context.Context, event *larkim.P1P2PChatCreatedV1) error {
			if event == nil || event.Event == nil {
				return nil
			}
			openID := ""
			userName := "伙伴"
			if event.Event.User != nil {
				openID = event.Event.User.OpenId
				if event.Event.User.Name != "" {
					userName = event.Event.User.Name
					s.CacheUserName(openID, userName)
				}
			}
			chatID := event.Event.ChatID
			fmt.Printf("[Feishu] 收到用户单聊建立事件 (p2p_chat_create): 用户=%s, OpenID=%s, ChatID=%s\n", userName, openID, chatID)
			if chatID != "" {
				card := BuildWelcomeP2PCard(userName, openID, chatID)
				_, _ = s.SendCard(ctx, larkim.CreateMessageV1ReceiveIDTypeChatId, chatID, card)
			}
			return nil
		}).
		// 处理机器人被拉入群聊事件
		OnP2ChatMemberBotAddedV1(func(ctx context.Context, event *larkim.P2ChatMemberBotAddedV1) error {
			if event == nil || event.Event == nil {
				return nil
			}
			chatID := ""
			chatName := "本群"
			if event.Event.ChatId != nil {
				chatID = *event.Event.ChatId
			}
			if event.Event.Name != nil && *event.Event.Name != "" {
				chatName = *event.Event.Name
			}
			fmt.Printf("[Feishu] 机器人已加入群聊: 群名=%s, ChatID=%s\n", chatName, chatID)
			if chatID != "" {
				card := BuildWelcomeGroupCard(chatName, chatID)
				_, _ = s.SendCard(ctx, larkim.CreateMessageV1ReceiveIDTypeChatId, chatID, card)
			}
			return nil
		}).
		// 处理用户进入单聊事件
		OnP2ChatAccessEventBotP2pChatEnteredV1(func(_ context.Context, _ *larkim.P2ChatAccessEventBotP2pChatEnteredV1) error {
			return nil
		}).
		// 群组生命周期相关事件
		OnP2ChatDisbandedV1(func(_ context.Context, _ *larkim.P2ChatDisbandedV1) error { return nil }).
		OnP2ChatMemberBotDeletedV1(func(_ context.Context, _ *larkim.P2ChatMemberBotDeletedV1) error { return nil }).
		OnP2ChatMemberUserAddedV1(func(_ context.Context, _ *larkim.P2ChatMemberUserAddedV1) error { return nil }).
		OnP2ChatMemberUserDeletedV1(func(_ context.Context, _ *larkim.P2ChatMemberUserDeletedV1) error { return nil }).
		OnP2ChatMemberUserWithdrawnV1(func(_ context.Context, _ *larkim.P2ChatMemberUserWithdrawnV1) error { return nil }).
		OnP2ChatUpdatedV1(func(_ context.Context, _ *larkim.P2ChatUpdatedV1) error { return nil }).
		// 消息状态与表情互动事件
		OnP2MessageReadV1(func(_ context.Context, _ *larkim.P2MessageReadV1) error { return nil }).
		OnP2MessageRecalledV1(func(_ context.Context, _ *larkim.P2MessageRecalledV1) error { return nil }).
		OnP2MessageReactionCreatedV1(func(_ context.Context, _ *larkim.P2MessageReactionCreatedV1) error { return nil }).
		OnP2MessageReactionDeletedV1(func(_ context.Context, _ *larkim.P2MessageReactionDeletedV1) error { return nil }).
		// 交互回调与自定义事件
		OnP2CardActionTrigger(func(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
			s.mu.RLock()
			inq := s.Inquiry
			s.mu.RUnlock()
			if inq != nil {
				return inq.HandleCardAction(ctx, event)
			}
			return &callback.CardActionTriggerResponse{}, nil
		}).
		OnP2CardURLPreviewGet(func(_ context.Context, _ *callback.URLPreviewGetEvent) (*callback.URLPreviewGetResponse, error) {
			return &callback.URLPreviewGetResponse{}, nil
		}).
		OnCustomizedEvent("im.message.bot_muted_v1", func(_ context.Context, _ *larkevent.EventReq) error { return nil }).
		OnCustomizedEvent("profile.view.get", func(_ context.Context, _ *larkevent.EventReq) error { return nil })

	s.dispatcher = disp
	s.webhookHandler = httpserverext.NewEventHandlerFunc(disp)

	// 3. 启动官方 WebSocket 长连接网关（免公网IP）
	if s.Gateway != nil {
		_ = s.Gateway.Start(s.cfg.AppID, secretVal, disp)
	}
}

// Status 获取飞书集成运行与连通状态（具备 30 秒缓存）
func (s *Service) Status(ctx context.Context, forceRefresh bool) *StatusResult {
	s.mu.RLock()
	if !forceRefresh && s.cachedStatus != nil && time.Since(s.lastStatusCheck) < 30*time.Second {
		res := *s.cachedStatus
		s.mu.RUnlock()
		return &res
	}
	cfg := s.cfg
	client := s.larkClient
	gw := s.Gateway
	s.mu.RUnlock()

	now := time.Now().UTC()
	if cfg.AppID == "" || cfg.AppSecretRef == "" {
		res := &StatusResult{
			Configured:    false,
			Enabled:       cfg.Enabled,
			Connected:     false,
			AppID:         cfg.AppID,
			Error:         "尚未配置 App ID 或 App Secret",
			LastCheckedAt: now,
		}
		s.cacheStatus(res)
		return res
	}

	if !cfg.Enabled {
		res := &StatusResult{
			Configured:    true,
			Enabled:       false,
			Connected:     false,
			AppID:         cfg.AppID,
			Error:         "飞书应用协同集成已停用",
			LastCheckedAt: now,
		}
		s.cacheStatus(res)
		return res
	}

	secretVal, err := s.GetAppSecret()
	if err != nil {
		res := &StatusResult{
			Configured:    true,
			Enabled:       true,
			Connected:     false,
			AppID:         cfg.AppID,
			Error:         "读取 App Secret 失败: " + err.Error(),
			LastCheckedAt: now,
		}
		s.cacheStatus(res)
		return res
	}

	if client == nil {
		client = lark.NewClient(cfg.AppID, secretVal, lark.WithLogLevel(larkcore.LogLevelInfo))
	}

	start := time.Now()
	tokResp, err := client.GetTenantAccessTokenBySelfBuiltApp(ctx, &larkcore.SelfBuiltTenantAccessTokenReq{
		AppID:     cfg.AppID,
		AppSecret: secretVal,
	})
	latency := time.Since(start).Milliseconds()

	var res *StatusResult
	if err != nil {
		res = &StatusResult{
			Configured:    true,
			Enabled:       true,
			Connected:     false,
			AppID:         cfg.AppID,
			LatencyMs:     latency,
			Error:         err.Error(),
			LastCheckedAt: now,
		}
	} else if tokResp.Code != 0 {
		res = &StatusResult{
			Configured:    true,
			Enabled:       true,
			Connected:     false,
			AppID:         cfg.AppID,
			LatencyMs:     latency,
			Error:         fmt.Sprintf("飞书鉴权失败 [%d]: %s", tokResp.Code, tokResp.Msg),
			LastCheckedAt: now,
		}
	} else {
		res = &StatusResult{
			Configured:     true,
			Enabled:        true,
			Connected:      true,
			AppID:          cfg.AppID,
			BotName:        "飞书自建应用机器人",
			ActivateStatus: 2,
			LatencyMs:      latency,
			LastCheckedAt:  now,
		}
	}

	if gw != nil {
		wsSt := gw.Status()
		res.WSState = wsSt.State
		res.WSError = wsSt.ErrorMessage
	}

	s.cacheStatus(res)
	return res
}

func (s *Service) cacheStatus(res *StatusResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cachedStatus = res
	s.lastStatusCheck = res.LastCheckedAt
}

// SendMessage 基于官方 SDK 发送消息至飞书用户或群聊
// content 可以是纯文本字符串，也可以是消息卡片 JSON（自动检测）
func (s *Service) SendMessage(ctx context.Context, receiveIDType, receiveID, content string) (*SendResult, error) {
	s.mu.RLock()
	client := s.larkClient
	cfg := s.cfg
	s.mu.RUnlock()

	if cfg.AppID == "" || cfg.AppSecretRef == "" {
		return nil, fmt.Errorf("飞书应用凭证未配置完整")
	}
	if !cfg.Enabled {
		return nil, fmt.Errorf("飞书应用协同尚未启用")
	}

	if client == nil {
		secretVal, err := s.GetAppSecret()
		if err != nil {
			return nil, fmt.Errorf("读取 App Secret 失败: %w", err)
		}
		client = lark.NewClient(cfg.AppID, secretVal, lark.WithLogLevel(larkcore.LogLevelInfo))
	}

	if receiveID == "" {
		return nil, fmt.Errorf("接收对象 receive_id 不能为空")
	}
	if receiveIDType == "" {
		if strings.HasPrefix(receiveID, "ou_") {
			receiveIDType = larkim.CreateMessageV1ReceiveIDTypeOpenId
		} else if strings.HasPrefix(receiveID, "oc_") {
			receiveIDType = larkim.CreateMessageV1ReceiveIDTypeChatId
		} else {
			receiveIDType = larkim.CreateMessageV1ReceiveIDTypeOpenId
		}
	}

	// 根据 content 内容自动判断消息类型：卡片 JSON 使用 interactive，否则使用 text
	msgType := larkim.MsgTypeText
	contentPayload := ""

	if content == "" {
		card := BuildTestMessageCard("")
		msgType = larkim.MsgTypeInteractive
		contentPayload = card.MustJSON()
	} else if IsCardJSON(content) {
		msgType = larkim.MsgTypeInteractive
		contentPayload = content
	} else {
		contentObj := map[string]string{"text": content}
		contentBytes, _ := json.Marshal(contentObj)
		contentPayload = string(contentBytes)
	}

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType(msgType).
			Content(contentPayload).
			Build()).
		Build()

	resp, err := client.Im.Message.Create(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("发送飞书消息失败: %w", err)
	}
	if !resp.Success() {
		return nil, fmt.Errorf("飞书发信失败 [%d]: %s", resp.Code, resp.Msg)
	}

	msgID := ""
	chatID := ""
	if resp.Data != nil {
		if resp.Data.MessageId != nil {
			msgID = *resp.Data.MessageId
		}
		if resp.Data.ChatId != nil {
			chatID = *resp.Data.ChatId
		}
	}

	return &SendResult{
		MessageID: msgID,
		ChatID:    chatID,
		SentAt:    time.Now().UTC(),
	}, nil
}

// SendCard 发送消息卡片至飞书用户或群聊（Card 对象版，自动序列化为 JSON）
func (s *Service) SendCard(ctx context.Context, receiveIDType, receiveID string, card *Card) (*SendResult, error) {
	cardJSON := card.MustJSON()
	if cardJSON == "" {
		return nil, fmt.Errorf("卡片序列化失败")
	}
	return s.SendMessage(ctx, receiveIDType, receiveID, cardJSON)
}

// TestSendMessage 发送测试消息（使用消息卡片格式）
func (s *Service) TestSendMessage(ctx context.Context, receiveIDType, receiveID, content string) (*SendResult, error) {
	card := BuildTestMessageCard(content)
	return s.SendCard(ctx, receiveIDType, receiveID, card)
}

// SDKSender 实现 Sender 接口，基于统一 SendMessage 发送终态出站通知
type SDKSender struct {
	svc *Service
}

// NewSDKSender 创建出站发送器
func NewSDKSender(svc *Service) *SDKSender {
	return &SDKSender{svc: svc}
}

// Send 发送回复消息
func (s *SDKSender) Send(ctx context.Context, reply Reply) error {
	if reply.ChatID == "" {
		return nil
	}
	receiveType := larkim.CreateMessageV1ReceiveIDTypeChatId
	if strings.HasPrefix(reply.ChatID, "ou_") {
		receiveType = larkim.CreateMessageV1ReceiveIDTypeOpenId
	}
	_, err := s.svc.SendMessage(ctx, receiveType, reply.ChatID, reply.Content)
	return err
}

// ListBindings 全部绑定（Admin）
func (s *Service) ListBindings() []Binding {
	s.mu.RLock()
	defer s.mu.RUnlock()
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

// BindingByEmployee 按 Employee 查绑定
func (s *Service) BindingByEmployee(employeeID string) *Binding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, b := range s.bindings {
		if b.EmployeeID == employeeID {
			cp := b
			return &cp
		}
	}
	return nil
}

// ResolveTargetChat 获取该数字员工关联的默认飞书通知目标。
// 优先使用绑定的飞书 open_id，未设置则使用默认群聊 chat_id。
func (s *Service) ResolveTargetChat(employeeID string) string {
	b := s.BindingByEmployee(employeeID)
	if b == nil {
		return ""
	}
	if strings.TrimSpace(b.FeishuOpenID) != "" {
		return strings.TrimSpace(b.FeishuOpenID)
	}
	return strings.TrimSpace(b.ChatID)
}

// CacheUserName 主动缓存 OpenID 对应的用户姓名
func (s *Service) CacheUserName(openID, name string) {
	openID = strings.TrimSpace(openID)
	name = strings.TrimSpace(name)
	if openID != "" && name != "" {
		s.userNameCache.Store(openID, name)
	}
}

// ResolveUserName 根据 OpenID 获取飞书人员真实姓名。
// 优先读内存缓存，未命中则请求飞书通讯录接口获取；未获取到时返回空字符串（以便调用方回退显示原始 ID）。
func (s *Service) ResolveUserName(ctx context.Context, openID string) string {
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return ""
	}
	if val, ok := s.userNameCache.Load(openID); ok {
		if name, ok := val.(string); ok && name != "" {
			return name
		}
	}

	s.mu.RLock()
	client := s.larkClient
	cfg := s.cfg
	s.mu.RUnlock()

	if client == nil && cfg.AppID != "" && cfg.AppSecretRef != "" {
		if secretVal, err := s.GetAppSecret(); err == nil && secretVal != "" {
			client = lark.NewClient(cfg.AppID, secretVal, lark.WithLogLevel(larkcore.LogLevelInfo))
		}
	}
	if client == nil {
		return ""
	}

	req := larkcontact.NewGetUserReqBuilder().
		UserId(openID).
		UserIdType("open_id").
		Build()

	resp, err := client.Contact.User.Get(ctx, req)
	if err == nil && resp.Success() && resp.Data != nil && resp.Data.User != nil && resp.Data.User.Name != nil {
		name := strings.TrimSpace(*resp.Data.User.Name)
		if name != "" {
			s.userNameCache.Store(openID, name)
			return name
		}
	}
	return ""
}

// saveBindingsLocked 持久化绑定到 Vault
func (s *Service) saveBindingsLocked() {
	if s.vault == nil {
		return
	}
	seen := map[string]struct{}{}
	list := make([]Binding, 0, len(s.bindings))
	for _, b := range s.bindings {
		key := b.EmployeeID + "|" + b.FeishuAlias
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			list = append(list, b)
		}
	}
	if b, err := json.Marshal(list); err == nil {
		if rot, ok := s.vault.(secret.Rotator); ok {
			for _, ref := range s.vault.List() {
				if ref.Name == "feishu.bindings_data" {
					_ = rot.Delete(ref.ID)
				}
			}
		}
		_, _ = s.vault.Put("feishu.bindings_data", string(b))
	}
}

// UpsertBinding 绑定别名
func (s *Service) UpsertBinding(b Binding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, existing := range s.bindings {
		if existing.EmployeeID == b.EmployeeID {
			delete(s.bindings, k)
			if existing.FeishuOpenID != "" {
				s.unindexOpenIDLocked(existing.FeishuOpenID, existing.EmployeeID)
			}
		}
	}
	if b.FeishuAlias != "" {
		s.bindings[aliasKey(b.FeishuAlias)] = b
	} else if b.EmployeeID != "" {
		s.bindings[aliasKey(b.EmployeeID)] = b
	}
	if b.FeishuOpenID != "" {
		s.indexOpenIDLocked(b.FeishuOpenID, b.EmployeeID)
	}
	s.saveBindingsLocked()
}

func (s *Service) indexOpenIDLocked(openID, employeeID string) {
	if openID == "" || employeeID == "" {
		return
	}
	for _, id := range s.byOpenID[openID] {
		if id == employeeID {
			return
		}
	}
	s.byOpenID[openID] = append(s.byOpenID[openID], employeeID)
}

func (s *Service) unindexOpenIDLocked(openID, employeeID string) {
	ids := s.byOpenID[openID]
	kept := ids[:0]
	for _, id := range ids {
		if id != employeeID {
			kept = append(kept, id)
		}
	}
	if len(kept) == 0 {
		delete(s.byOpenID, openID)
		return
	}
	s.byOpenID[openID] = kept
}

// IdentityNotice 本次 @ 消息里要告知对方的飞书身份。
type IdentityNotice struct {
	OpenID       string
	ChatID       string
	Group        bool
	OpenBoundNow bool
	ChatBoundNow bool
	SavedChatID  string
}

// AutoBindFromMention 缺什么补什么：群聊补 OpenID 和默认群聊，单聊只补 OpenID。
func (s *Service) AutoBindFromMention(employeeID, senderOpenID, chatID string, group bool) IdentityNotice {
	note := IdentityNotice{OpenID: strings.TrimSpace(senderOpenID), Group: group}
	if group {
		note.ChatID = strings.TrimSpace(chatID)
	}
	existing := s.BindingByEmployee(employeeID)
	if existing == nil {
		next := Binding{EmployeeID: employeeID, FeishuOpenID: note.OpenID}
		if group {
			next.ChatID = note.ChatID
		}
		note.OpenBoundNow = note.OpenID != ""
		note.ChatBoundNow = group && note.ChatID != ""
		note.SavedChatID = next.ChatID
		if note.OpenBoundNow || note.ChatBoundNow {
			s.UpsertBinding(next)
		}
		return note
	}
	next := *existing
	if next.FeishuOpenID == "" && note.OpenID != "" {
		next.FeishuOpenID = note.OpenID
		note.OpenBoundNow = true
	} else if next.FeishuOpenID != "" {
		note.OpenID = next.FeishuOpenID
	}
	if group && next.ChatID == "" && note.ChatID != "" {
		next.ChatID = note.ChatID
		note.ChatBoundNow = true
	}
	note.SavedChatID = next.ChatID
	if !group {
		note.ChatID = next.ChatID
	}
	if note.OpenBoundNow || note.ChatBoundNow {
		s.UpsertBinding(next)
	}
	return note
}

// FormatIdentityNotice 写成回执里的身份说明。
func FormatIdentityNotice(n IdentityNotice) string {
	if n.OpenID == "" && n.ChatID == "" && n.SavedChatID == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("**你的飞书身份**\n")
	if n.OpenID != "" {
		sb.WriteString(fmt.Sprintf("• OpenID：`%s`", n.OpenID))
		if n.OpenBoundNow {
			sb.WriteString("（本次已自动绑定）")
		} else {
			sb.WriteString("（已绑定）")
		}
		sb.WriteString("\n")
	}
	chat := n.ChatID
	if !n.Group {
		chat = n.SavedChatID
	}
	if n.Group {
		if chat != "" {
			sb.WriteString(fmt.Sprintf("• 群聊 Chat ID：`%s`", chat))
			if n.ChatBoundNow {
				sb.WriteString("（本次已自动绑定为默认群聊）")
			} else {
				sb.WriteString("（已绑定）")
			}
			sb.WriteString("\n")
		}
	} else if chat != "" {
		sb.WriteString(fmt.Sprintf("• 默认群聊 Chat ID：`%s`（已绑定，单聊不会改写）\n", chat))
	} else {
		sb.WriteString("• 默认群聊 Chat ID：单聊不绑定，请在群里 @该员工后自动写入\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// DeleteBinding 删除绑定
func (s *Service) DeleteBinding(employeeID, alias string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := false
	if alias != "" {
		key := aliasKey(alias)
		if b, ok := s.bindings[key]; ok {
			if employeeID == "" || b.EmployeeID == employeeID {
				delete(s.bindings, key)
				if b.FeishuOpenID != "" {
					s.unindexOpenIDLocked(b.FeishuOpenID, b.EmployeeID)
				}
				removed = true
			}
		}
		if removed {
			s.saveBindingsLocked()
		}
		return removed
	}
	if employeeID == "" {
		return false
	}
	for k, b := range s.bindings {
		if b.EmployeeID == employeeID {
			delete(s.bindings, k)
			if b.FeishuOpenID != "" {
				s.unindexOpenIDLocked(b.FeishuOpenID, b.EmployeeID)
			}
			removed = true
		}
	}
	if removed {
		s.saveBindingsLocked()
	}
	return removed
}

// Dedupe 事件去重
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
	reLeadingMentions = regexp.MustCompile(`^(\s*@[\p{L}\p{N}_-]+\s*)+`)
	reAllMentions     = regexp.MustCompile(`(?i)@([\p{L}\p{N}_-]+)`)
	reEmpID           = regexp.MustCompile(`(?i)\b(EMP-[a-zA-Z0-9-]+)\b`)
	reSlash           = regexp.MustCompile(`(?i)^/(?:emp|employee)\s+(\S+)\s+(.+)$`)
)

// normalizeMentionText 去掉零宽/格式字符，并把全角 @、全角数字折成半角，避免「可乐1」对不上。
func normalizeMentionText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.Is(unicode.Cf, r) {
			continue
		}
		switch {
		case r == '＠':
			r = '@'
		case r >= '０' && r <= '９':
			r = '0' + (r - '０')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// aliasKey 绑定表与查找共用的别名键。
func aliasKey(s string) string {
	return strings.ToLower(strings.TrimSpace(normalizeMentionText(s)))
}

// runeCodes 把文本打成码点，便于对照飞书原文和库里的别名。
func runeCodes(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "U+%04X", r)
	}
	return b.String()
}

// appendMentionNames 把飞书 mention 的显示名追加进文本。占位符没被替换时仍能按真实别名匹配。
func appendMentionNames(text string, mentions []*larkim.MentionEvent) string {
	if len(mentions) == 0 {
		return text
	}
	var b strings.Builder
	b.WriteString(text)
	for _, m := range mentions {
		if m == nil || m.Name == nil {
			continue
		}
		name := strings.TrimSpace(*m.Name)
		if name == "" {
			continue
		}
		b.WriteString(" @")
		b.WriteString(name)
	}
	return b.String()
}

// CleanPrompt 剔除开头的全部 @提及 标记（例如 "@AI员工 @合并"），返回纯净的任务指令
func (s *Service) CleanPrompt(text string) string {
	text = strings.TrimSpace(text)
	cleaned := strings.TrimSpace(reLeadingMentions.ReplaceAllString(text, ""))
	if cleaned != "" {
		return cleaned
	}
	return text
}

// ResolveMentions 将飞书文本中的 @_user_x 占位符还原为真实姓名/别名
func (s *Service) ResolveMentions(rawText string, mentions []*larkim.MentionEvent) string {
	if len(mentions) == 0 || rawText == "" {
		return rawText
	}
	result := rawText
	for _, m := range mentions {
		if m == nil || m.Key == nil || *m.Key == "" {
			continue
		}
		key := *m.Key
		name := ""
		if m.Name != nil && *m.Name != "" {
			name = *m.Name
			if m.Id != nil && m.Id.OpenId != nil && *m.Id.OpenId != "" {
				s.CacheUserName(*m.Id.OpenId, name)
			}
		}
		if name != "" {
			replacement := "@" + name
			result = strings.ReplaceAll(result, key, replacement)
		}
	}
	return result
}

// ParseTarget 从文本解析目标 Employee 与 Prompt。
// 群聊里通常会先 @机器人，再写 /emp 或别名，开头的 @提及不参与 /emp 匹配。
func (s *Service) ParseTarget(text string) (employeeID, prompt string, err error) {
	text = strings.TrimSpace(normalizeMentionText(text))
	body := strings.TrimSpace(reLeadingMentions.ReplaceAllString(text, ""))
	if body == "" {
		body = text
	}
	if m := reSlash.FindStringSubmatch(body); len(m) == 3 {
		key := m[1]
		prompt = s.CleanPrompt(m[2])
		if strings.HasPrefix(strings.ToUpper(key), "EMP-") {
			return strings.ToUpper(key), prompt, nil
		}
		s.mu.RLock()
		b, ok := s.bindings[aliasKey(key)]
		s.mu.RUnlock()
		if !ok {
			return "", "", ErrNoEmployee
		}
		return b.EmployeeID, prompt, nil
	}
	if m := reEmpID.FindStringSubmatch(body); len(m) == 2 {
		id := strings.ToUpper(m[1])
		p := strings.TrimSpace(reEmpID.ReplaceAllString(text, ""))
		prompt = s.CleanPrompt(p)
		return id, prompt, nil
	}
	// 多个别名同时命中时取最长的，避免「可乐」截走「可乐1」。
	allMentions := reAllMentions.FindAllStringSubmatch(text, -1)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var hit Binding
	hitLen := -1
	for _, match := range allMentions {
		if len(match) != 2 {
			continue
		}
		alias := aliasKey(match[1])
		b, ok := s.bindings[alias]
		if !ok || len(alias) <= hitLen {
			continue
		}
		hit = b
		hitLen = len(alias)
	}
	if hitLen >= 0 {
		return hit.EmployeeID, s.CleanPrompt(text), nil
	}
	return "", "", ErrNoEmployee
}

// FetchMessageContent 根据 messageID 调用飞书 SDK 拉取消息内容（支持 text、post、interactive 等格式）
func (s *Service) FetchMessageContent(ctx context.Context, messageID string) (string, error) {
	if messageID == "" {
		return "", nil
	}
	s.mu.RLock()
	client := s.larkClient
	s.mu.RUnlock()
	if client == nil {
		return "", fmt.Errorf("larkClient 未就绪")
	}

	req := larkim.NewGetMessageReqBuilder().
		MessageId(messageID).
		Build()

	resp, err := client.Im.Message.Get(ctx, req)
	if err != nil {
		return "", fmt.Errorf("获取父消息失败: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("获取父消息接口报错 [%d]: %s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || len(resp.Data.Items) == 0 {
		return "", nil
	}

	item := resp.Data.Items[0]
	msgType := ""
	if item.MsgType != nil {
		msgType = *item.MsgType
	}
	contentStr := ""
	if item.Body != nil && item.Body.Content != nil {
		contentStr = *item.Body.Content
	}

	return ParseMessageBody(msgType, contentStr), nil
}

// isP2PChat 判断是否为飞书私聊（单聊）。
func isP2PChat(chatType string) bool {
	ct := strings.ToLower(strings.TrimSpace(chatType))
	return ct == "p2p" || ct == "private"
}

// looksLikeEmployeeAddress 文本像是在呼叫数字员工，但可能格式或别名不对。
func looksLikeEmployeeAddress(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if strings.HasPrefix(text, "/") {
		return true
	}
	return reAllMentions.MatchString(text) || reEmpID.MatchString(text)
}

// boundAliases 返回当前已绑定的员工别名列表（去重、保序）。
func (s *Service) boundAliases() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]struct{}{}
	out := make([]string, 0, len(s.bindings))
	for _, b := range s.bindings {
		alias := strings.TrimSpace(b.FeishuAlias)
		if alias == "" {
			continue
		}
		key := aliasKey(alias)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

// EmployeeByOpenID 按飞书用户 OpenID 查已绑定的数字员工。
func (s *Service) EmployeeByOpenID(openID string) string {
	if strings.TrimSpace(openID) == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.byOpenID[openID]
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

// EmployeeHasOpenID 该员工是否已绑定这个飞书用户。同一 OpenID 可以绑多个员工。
func (s *Service) EmployeeHasOpenID(employeeID, openID string) bool {
	openID = strings.TrimSpace(openID)
	if employeeID == "" || openID == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, id := range s.byOpenID[openID] {
		if id == employeeID {
			return true
		}
	}
	return false
}

// HandleMessage 异步创建 Message/Job 后返回。
// 派单规则：
//  1. 必须显式 @别名 / EMP-xxx / /emp 才能派单（不做 open_id / chat_id / 唯一绑定兜底）；
//  2. 群聊里出现了 @、/emp 或 EMP- 但没对上员工：回复格式提示；完全不像呼叫则静默；
//  3. 私聊未点名员工：回复「请@对应员工执行」，不创建任务。
func (s *Service) HandleMessage(ctx context.Context, ev IncomingEvent) (jobID string, duplicate bool, err error) {
	if s.Dedupe(ev.EventID) {
		return "", true, nil
	}

	// 优先检查是否为针对待确认决策/安全选项的直接文本回复
	s.mu.RLock()
	inq := s.Inquiry
	s.mu.RUnlock()
	if inq != nil {
		handled, inqErr := inq.CheckChatReply(ctx, ev.ChatID, ev.Text, ev.SenderOpenID)
		if handled {
			return "", false, inqErr
		}
	}

	empID, prompt, err := s.ParseTarget(ev.Text)
	if err != nil || empID == "" {
		fmt.Printf("[Feishu] 未匹配员工 text=%q codes=%s aliases=%v\n", ev.Text, runeCodes(ev.Text), s.boundAliases())
		shouldHint := isP2PChat(ev.ChatType) || looksLikeEmployeeAddress(ev.Text)
		if shouldHint && s.Sender != nil && ev.ChatID != "" {
			card := BuildMentionRequiredCard(s.boundAliases())
			_ = s.Sender.Send(ctx, Reply{ChatID: ev.ChatID, Content: card.MustJSON()})
		}
		return "", false, ErrNoEmployee
	}

	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = s.CleanPrompt(ev.Text)
	}
	if prompt == "" {
		prompt = ev.Text
	}

	// 组装最终派发给工作站的完整 prompt（注入引用/上下文）
	fullPrompt := prompt
	if ev.QuotedContent != "" {
		fullPrompt = fmt.Sprintf("【引用/上下文内容如下】：\n%s\n\n【任务指令】：\n%s", strings.TrimSpace(ev.QuotedContent), prompt)
	}

	if s.Employees != nil {
		if err := s.Employees.IsAssignable(ctx, empID); err != nil {
			if s.Sender != nil && ev.ChatID != "" {
				empName := s.getEmployeeDisplayName(ctx, empID)
				card := BuildNotAssignableCard(empName, err)
				_ = s.Sender.Send(ctx, Reply{ChatID: ev.ChatID, Content: card.MustJSON()})
			}
			return "", false, err
		}
	}
	idem := fmt.Sprintf("feishu:%s:%s", ev.SenderOpenID, ev.MessageID)
	if ev.MessageID == "" {
		idem = fmt.Sprintf("feishu:%s:%s", ev.SenderOpenID, ev.EventID)
	}
	group := !isP2PChat(ev.ChatType)
	notice := FormatIdentityNotice(s.AutoBindFromMention(empID, ev.SenderOpenID, ev.ChatID, group))
	if s.Jobs == nil {
		return "", false, errors.New("未配置 JobCreator")
	}
	jobID, err = s.Jobs.CreateFromFeishu(ctx, empID, fullPrompt, idem, ev.ChatID, ev.MessageID, ev.SenderOpenID)
	if err != nil {
		if s.Sender != nil && ev.ChatID != "" {
			card := BuildTaskFailedCard(err, notice)
			_ = s.Sender.Send(ctx, Reply{ChatID: ev.ChatID, Content: card.MustJSON()})
		}
		return "", false, err
	}

	// 任务创建成功即时回复应答
	if s.Sender != nil && ev.ChatID != "" {
		instructionPreview := prompt
		if ev.QuotedContent != "" {
			instructionPreview = fmt.Sprintf("%s (已带入引用的上下文内容)", prompt)
		}
		empName := s.getEmployeeDisplayName(ctx, empID)
		card := BuildTaskCreatedCard(jobID, empName, instructionPreview, notice)
		_ = s.Sender.Send(ctx, Reply{ChatID: ev.ChatID, Content: card.MustJSON()})
	}

	return jobID, false, nil
}

// getEmployeeDisplayName 获取用于对用户展示的员工名称（优先使用员工系统名称，其次使用绑定的别名，兜底才使用 empID）
func (s *Service) getEmployeeDisplayName(ctx context.Context, empID string) string {
	if s.Employees != nil {
		if name := s.Employees.GetEmployeeName(ctx, empID); name != "" {
			return name
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, b := range s.bindings {
		if b.EmployeeID == empID && b.FeishuAlias != "" {
			return b.FeishuAlias
		}
	}
	return empID
}

// NotifyJobResult 终态回复飞书（使用消息卡片格式）
func (s *Service) NotifyJobResult(ctx context.Context, chatID, jobID, status, summary string) error {
	if s.Sender == nil || chatID == "" {
		return nil
	}
	card := BuildJobResultCard(jobID, status, summary)
	return s.Sender.Send(ctx, Reply{ChatID: chatID, Content: card.MustJSON()})
}

// HandleURLChallenge 处理飞书旧版 URL 校验（兼容老测试用例）
func (s *Service) HandleURLChallenge(token, challenge string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.VerificationToken != "" && token != s.cfg.VerificationToken {
		return "", ErrBadSignature
	}
	return challenge, nil
}

// VerifySignature 校验请求签名（兼容老测试用例）
func (s *Service) VerifySignature(timestamp, nonce, signature, body string) error {
	s.mu.RLock()
	token := s.cfg.VerificationToken
	s.mu.RUnlock()
	if token == "" {
		return ErrBadSignature
	}
	sum := sha256.Sum256([]byte(timestamp + nonce + token + body))
	expect := hex.EncodeToString(sum[:])
	if !strings.EqualFold(expect, signature) {
		return ErrBadSignature
	}
	return nil
}

// VerifyToken 校验请求凭据是否与配置的 VerificationToken 匹配
func (s *Service) VerifyToken(token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.VerificationToken == "" {
		return false
	}
	return token != "" && token == s.cfg.VerificationToken
}

// MemorySender 测试用
type MemorySender struct {
	mu   sync.Mutex
	Sent []Reply
}

func (m *MemorySender) Send(_ context.Context, reply Reply) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Sent = append(m.Sent, reply)
	return nil
}

// extractTextContent 从消息内容反序列化纯文本（飞书传入 content 多为 JSON 字符串形如 {"text":"..."}）
func extractTextContent(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "{") {
		var obj struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(raw), &obj); err == nil && obj.Text != "" {
			return obj.Text
		}
	}
	return raw
}

// truncateStr 截断过长字符串便于日志打印
func truncateStr(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// ParseMessageBody 解析飞书消息体 JSON，提取出纯文本或 Markdown 内容
func ParseMessageBody(msgType, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, "{") {
		return raw
	}

	// 1. 尝试直接作为文本消息 {"text": "..."} 解析
	var textObj struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &textObj); err == nil && textObj.Text != "" {
		return textObj.Text
	}

	// 2. 尝试作为 post 富文本解析
	var genericMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &genericMap); err == nil {
		// 直接包含 content 字段
		if _, hasContent := genericMap["content"]; hasContent {
			if parsed := parsePostContent(genericMap); parsed != "" {
				return parsed
			}
		}
		// 语言包装，如 zh_cn, en_us 等
		for _, langData := range genericMap {
			var langMap map[string]json.RawMessage
			if err := json.Unmarshal(langData, &langMap); err == nil {
				if _, hasContent := langMap["content"]; hasContent {
					if parsed := parsePostContent(langMap); parsed != "" {
						return parsed
					}
				}
			}
		}
		// 检查是否为 interactive 消息卡片
		if parsed := parseInteractiveCard(genericMap); parsed != "" {
			return parsed
		}
	}

	return raw
}

func parsePostContent(m map[string]json.RawMessage) string {
	var title string
	if tRaw, ok := m["title"]; ok {
		_ = json.Unmarshal(tRaw, &title)
	}

	var contentRows [][]map[string]interface{}
	if cRaw, ok := m["content"]; ok {
		_ = json.Unmarshal(cRaw, &contentRows)
	}

	var sb strings.Builder
	if title != "" {
		sb.WriteString(title)
		sb.WriteString("\n")
	}

	for _, row := range contentRows {
		var rowText strings.Builder
		for _, elem := range row {
			tag, _ := elem["tag"].(string)
			switch tag {
			case "text":
				if txt, ok := elem["text"].(string); ok {
					rowText.WriteString(txt)
				}
			case "a":
				txt, _ := elem["text"].(string)
				href, _ := elem["href"].(string)
				if txt != "" && href != "" {
					rowText.WriteString(fmt.Sprintf("[%s](%s)", txt, href))
				} else if txt != "" {
					rowText.WriteString(txt)
				} else if href != "" {
					rowText.WriteString(href)
				}
			case "at":
				userName, _ := elem["user_name"].(string)
				if userName != "" {
					rowText.WriteString("@" + userName)
				}
			}
		}
		if rowText.Len() > 0 {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(rowText.String())
		}
	}

	return strings.TrimSpace(sb.String())
}

func parseInteractiveCard(m map[string]json.RawMessage) string {
	var sb strings.Builder

	// Header / Title
	if hRaw, ok := m["header"]; ok {
		var header struct {
			Title struct {
				Content string `json:"content"`
			} `json:"title"`
		}
		if json.Unmarshal(hRaw, &header) == nil && header.Title.Content != "" {
			sb.WriteString(header.Title.Content)
			sb.WriteString("\n")
		}
	}

	// Elements
	if elRaw, ok := m["elements"]; ok {
		var elements []map[string]interface{}
		if json.Unmarshal(elRaw, &elements) == nil {
			for _, elem := range elements {
				if textObj, ok := elem["text"].(map[string]interface{}); ok {
					if content, ok := textObj["content"].(string); ok && content != "" {
						if sb.Len() > 0 {
							sb.WriteString("\n")
						}
						sb.WriteString(content)
					}
				} else if content, ok := elem["content"].(string); ok && content != "" {
					if sb.Len() > 0 {
						sb.WriteString("\n")
					}
					sb.WriteString(content)
				}
			}
		}
	}

	return strings.TrimSpace(sb.String())
}

// ParseWebhookBody 解析飞书 Webhook 入站请求体（兼容 URL verification 与各类消息事件）
func ParseWebhookBody(body []byte) (challenge string, token string, ev *IncomingEvent, err error) {
	var base struct {
		Type      string `json:"type"`
		Token     string `json:"token"`
		Challenge string `json:"challenge"`
		Header    struct {
			EventID   string `json:"event_id"`
			EventType string `json:"event_type"`
			Token     string `json:"token"`
		} `json:"header"`
		Event struct {
			Type    string `json:"type"`
			Message struct {
				ChatID    string `json:"chat_id"`
				ChatType  string `json:"chat_type"`
				MessageID string `json:"message_id"`
				ParentID  string `json:"parent_id"`
				Content   string `json:"content"`
			} `json:"message"`
			Sender struct {
				SenderID struct {
					OpenID string `json:"open_id"`
				} `json:"sender_id"`
			} `json:"sender"`
			// 兼容旧 v1 回调格式
			OpenID           string `json:"open_id"`
			ChatID           string `json:"chat_id"`
			ChatType         string `json:"chat_type"`
			Text             string `json:"text"`
			TextWithoutAtBot string `json:"text_without_at_bot"`
		} `json:"event"`
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(body, &base); err != nil {
		return "", "", nil, fmt.Errorf("解析请求体 JSON 失败: %w", err)
	}

	// 1. URL 校验
	if base.Type == "url_verification" {
		return base.Challenge, base.Token, nil, nil
	}

	tok := base.Token
	if tok == "" {
		tok = base.Header.Token
	}

	// 2. 消息事件 - v2 格式
	if base.Header.EventType == "im.message.receive_v1" || base.Event.Message.MessageID != "" {
		text := extractTextContent(base.Event.Message.Content)
		return "", tok, &IncomingEvent{
			EventID:      base.Header.EventID,
			MessageID:    base.Event.Message.MessageID,
			ParentID:     base.Event.Message.ParentID,
			ChatID:       base.Event.Message.ChatID,
			ChatType:     base.Event.Message.ChatType,
			SenderOpenID: base.Event.Sender.SenderID.OpenID,
			Text:         text,
			RawType:      base.Header.EventType,
		}, nil
	}

	// 兼容旧 v1 格式
	if base.Event.Type == "message" || base.Event.Text != "" || base.Event.TextWithoutAtBot != "" {
		t := base.Event.TextWithoutAtBot
		if t == "" {
			t = base.Event.Text
		}
		return "", tok, &IncomingEvent{
			EventID:      base.UUID,
			ChatID:       base.Event.ChatID,
			ChatType:     base.Event.ChatType,
			SenderOpenID: base.Event.OpenID,
			Text:         t,
			RawType:      "message",
		}, nil
	}

	return "", tok, nil, nil
}

package automation

import (
	"context"
	"encoding/json"
	"time"
)

// 触发类型。
const (
	TriggerCron     = "cron"
	TriggerCalendar = "calendar"
	TriggerWebhook  = "webhook"
)

// Run 状态。
const (
	RunTriggered  = "triggered"
	RunJobCreated = "job_created"
	RunSuccess    = "success"
	RunFailed     = "failed"
	RunSkipped    = "skipped"
)

// Automation 规则实体。
type Automation struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	TriggerType   string          `json:"trigger_type"`
	Enabled       bool            `json:"enabled"`
	EmployeeID    string          `json:"employee_id"`
	Prompt        string          `json:"prompt"`
	Timezone      string          `json:"timezone"`
	NotifyChatID  string          `json:"notify_chat_id"`
	TriggerConfig json.RawMessage `json:"trigger_config"`
	LastFiredAt   *time.Time      `json:"last_fired_at,omitempty"`
	CreatedBy     string          `json:"created_by"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// CalendarItem 日历日条目（同日按 seq 串行：成功后才触发下一条）。
type CalendarItem struct {
	ID           string    `json:"id"`
	AutomationID string    `json:"automation_id"`
	RunDate      string    `json:"run_date"` // YYYY-MM-DD
	Seq          int       `json:"seq"`
	EmployeeID   string    `json:"employee_id"`
	Prompt       string    `json:"prompt"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Run 一次触发记录。
type Run struct {
	ID             string          `json:"id"`
	AutomationID   string          `json:"automation_id"`
	CalendarItemID string          `json:"calendar_item_id,omitempty"`
	JobID          string          `json:"job_id,omitempty"`
	Status         string          `json:"status"`
	TriggerSource  string          `json:"trigger_source"`
	IdempotencyKey string          `json:"idempotency_key"`
	Error          string          `json:"error,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	FinishedAt     *time.Time      `json:"finished_at,omitempty"`
}

// CronConfig 周期触发配置。
type CronConfig struct {
	Expr   string `json:"expr"`
	Preset string `json:"preset,omitempty"` // daily|weekly|monthly|yearly|custom
}

// WebhookConfig Webhook 触发配置（密钥仅存引用）。
type WebhookConfig struct {
	PathToken       string   `json:"path_token"`
	AuthModes       []string `json:"auth_modes"` // hmac, bearer（至少一种）
	SecretRef       string   `json:"secret_ref,omitempty"`
	BearerRef       string   `json:"bearer_ref,omitempty"`
	IPAllowlist     []string `json:"ip_allowlist,omitempty"`
	RateLimitPerMin int      `json:"rate_limit_per_min"`
}

// CreateInput 创建规则。
type CreateInput struct {
	Name          string          `json:"name"`
	TriggerType   string          `json:"trigger_type"`
	Enabled       *bool           `json:"enabled,omitempty"`
	EmployeeID    string          `json:"employee_id"`
	Prompt        string          `json:"prompt"`
	Timezone      string          `json:"timezone"`
	NotifyChatID  string          `json:"notify_chat_id"`
	TriggerConfig json.RawMessage `json:"trigger_config"`
}

// UpdateInput 部分更新。
type UpdateInput struct {
	Name          *string          `json:"name,omitempty"`
	Enabled       *bool            `json:"enabled,omitempty"`
	EmployeeID    *string          `json:"employee_id,omitempty"`
	Prompt        *string          `json:"prompt,omitempty"`
	Timezone      *string          `json:"timezone,omitempty"`
	NotifyChatID  *string          `json:"notify_chat_id,omitempty"`
	TriggerConfig *json.RawMessage `json:"trigger_config,omitempty"`
}

// CalendarItemInput 写入日历条目（Enabled 默认 true）。
type CalendarItemInput struct {
	EmployeeID string `json:"employee_id"`
	Prompt     string `json:"prompt"`
	Enabled    *bool  `json:"enabled,omitempty"`
}

// FireRequest 一次待触发请求（由 Tick / Webhook / 日历链产生）。
type FireRequest struct {
	Automation     *Automation
	CalendarItem   *CalendarItem
	TriggerSource  string
	IdempotencyKey string
	Payload        map[string]string
}

// Trigger 可扩展触发源接口；Webhook 走 HTTP，不经 Due。
type Trigger interface {
	Type() string
	Due(ctx context.Context, now time.Time) ([]FireRequest, error)
}

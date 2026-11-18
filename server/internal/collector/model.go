package collector

import "time"

type Profile struct {
	UserID           string     `json:"user_id"`
	DisplayName      string     `json:"display_name"`
	Aliases          []string   `json:"aliases"`
	Timezone         string     `json:"timezone"`
	JiraURL          string     `json:"jira_url"`
	JiraAccount      string     `json:"jira_account"`
	JiraAuth         string     `json:"jira_auth"`
	MeegoUserKey     string     `json:"meego_user_key"`
	MeegoURL         string     `json:"meego_url"`
	MeegoPluginID    string     `json:"meego_plugin_id"`
	MeegoExpiresAt   *time.Time `json:"meego_expires_at,omitempty"`
	FeishuAppID      string     `json:"feishu_app_id"`
	FeishuOpenID     string     `json:"feishu_open_id"`
	FeishuName       string     `json:"feishu_name"`
	Scopes           string     `json:"scopes"`
	GrantedScopes    string     `json:"granted_scopes"`
	GrantID          string     `json:"grant_id,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	RefreshExpiresAt *time.Time `json:"refresh_expires_at,omitempty"`
	SecretRef        string     `json:"secret_ref,omitempty"`
	NotifyEnabled    bool       `json:"notify_enabled"`
	NotifyChatID     string     `json:"notify_chat_id"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type Credentials struct {
	AppSecret     string `json:"app_secret,omitempty"`
	AccessToken   string `json:"access_token,omitempty"`
	RefreshToken  string `json:"refresh_token,omitempty"`
	JiraToken     string `json:"jira_token,omitempty"`
	MeegoToken    string `json:"meego_token,omitempty"`
	MeegoSecret   string `json:"meego_secret,omitempty"`
	BotWebhook    string `json:"bot_webhook,omitempty"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
}

type ProfileInput struct {
	Profile
	Credentials      Credentials `json:"credentials"`
	ClearCredentials []string    `json:"clear_credentials"`
}

type Target struct {
	ID              string         `json:"id"`
	OwnerID         string         `json:"owner_id"`
	Name            string         `json:"name"`
	Source          string         `json:"source"`
	Enabled         bool           `json:"enabled"`
	URL             string         `json:"url"`
	IntervalMinutes int            `json:"interval_minutes"`
	Cron            string         `json:"cron"`
	Timezone        string         `json:"timezone"`
	NotifyBaseline  bool           `json:"notify_baseline"`
	Config          map[string]any `json:"config"`
	Version         int            `json:"version"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type Condition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}
type Rule struct {
	ID               string      `json:"id"`
	Name             string      `json:"name"`
	Enabled          bool        `json:"enabled"`
	Priority         int         `json:"priority"`
	Source           string      `json:"source"`
	TargetID         string      `json:"target_id"`
	Match            string      `json:"match"` // all | any
	Conditions       []Condition `json:"conditions"`
	Action           string      `json:"action"` // record | instant | batched
	AggregateMinutes int         `json:"aggregate_minutes"`
}

type Item struct {
	ID         string         `json:"id"`
	TargetID   string         `json:"target_id"`
	Source     string         `json:"source"`
	ExternalID string         `json:"external_id"`
	Title      string         `json:"title"`
	URL        string         `json:"url"`
	Status     string         `json:"status"`
	Done       bool           `json:"done"`
	DueDate    string         `json:"due_date"`
	Assignees  []string       `json:"assignees"`
	Fields     map[string]any `json:"fields"`
	Content    string         `json:"content,omitempty"`
	UpdatedAt  time.Time      `json:"updated_at"`
}
type Snapshot struct {
	Items     []Item    `json:"items"`
	ScannedAt time.Time `json:"scanned_at"`
	Version   int       `json:"version"`
}
type Event struct {
	ID            string         `json:"id"`
	Item          Item           `json:"item"`
	Kind          string         `json:"event_kind"`
	ChangedFields []string       `json:"changed_fields"`
	Before        map[string]any `json:"before,omitempty"`
	At            time.Time      `json:"occurred_at"`
	RuleID        string         `json:"rule_id"`
	Reason        string         `json:"reason"`
	Action        string         `json:"action"`
}
type Run struct {
	ID            string    `json:"id"`
	TargetID      string    `json:"target_id"`
	TargetName    string    `json:"target_name"`
	Source        string    `json:"source"`
	Preview       bool      `json:"preview"`
	Status        string    `json:"status"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at"`
	NextAt        time.Time `json:"next_at"`
	ConfigVersion int       `json:"config_version"`
	Count         int       `json:"count"`
	Changes       int       `json:"changes"`
	Error         string    `json:"error,omitempty"`
	Events        []Event   `json:"events"`
	Samples       []Item    `json:"samples"`
}
type Delivery struct {
	ID       string    `json:"id"`
	Events   []Event   `json:"events"`
	Status   string    `json:"status"`
	Attempts int       `json:"attempts"`
	NextAt   time.Time `json:"next_at"`
	Error    string    `json:"error,omitempty"`
}

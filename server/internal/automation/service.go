package automation

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ai-employee-platform/server/internal/idgen"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/secret"
)

// JobCreator 创建 Job。
type JobCreator interface {
	Create(ctx context.Context, in job.CreateInput, actorID, ip string) (*job.Job, bool, error)
	Get(ctx context.Context, id string) (*job.Job, error)
}

// JobScheduler 立刻尝试派发。
type JobScheduler interface {
	ScheduleJob(ctx context.Context, jobID string) (*job.Job, error)
}

// Service 自动化编排。
type Service struct {
	Store     Store
	Jobs      JobCreator
	Scheduler JobScheduler
	Vault     secret.Store
	Audit     Auditor
	Feishu    FeishuNotifier
	Notify    ChatBinder

	limiter *webhookLimiter
}

// New 创建服务。
func New(store Store, jobs JobCreator, sched JobScheduler, vault secret.Store, audit Auditor) *Service {
	return &Service{
		Store: store, Jobs: jobs, Scheduler: sched, Vault: vault, Audit: audit,
		limiter: newWebhookLimiter(),
	}
}

// SetNotify 注入飞书与 chat 绑定。
func (s *Service) SetNotify(fs FeishuNotifier, chat ChatBinder) {
	s.Feishu = fs
	s.Notify = chat
}

// List 列出规则。
func (s *Service) List(ctx context.Context, triggerType string) ([]*Automation, error) {
	return s.Store.List(ctx, triggerType)
}

// Get 获取规则。
func (s *Service) Get(ctx context.Context, id string) (*Automation, error) {
	return s.Store.Get(ctx, id)
}

// Create 创建规则；webhook 类型自动生成 path_token 与密钥。
// 返回 webhookSecrets 仅创建时可见（hmac_secret / bearer_token）。
func (s *Service) Create(ctx context.Context, in CreateInput, actorID string) (*Automation, map[string]string, error) {
	if in.Name == "" || in.TriggerType == "" {
		return nil, nil, ErrInvalidInput
	}
	switch in.TriggerType {
	case TriggerCron, TriggerCalendar, TriggerWebhook:
	default:
		return nil, nil, fmt.Errorf("%w: 未知 trigger_type", ErrInvalidInput)
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	tz := in.Timezone
	if tz == "" {
		tz = "Asia/Shanghai"
	}
	now := time.Now().UTC()
	a := &Automation{
		ID:           idgen.New("AUTO"),
		Name:         in.Name,
		TriggerType:  in.TriggerType,
		Enabled:      enabled,
		EmployeeID:   in.EmployeeID,
		Prompt:       in.Prompt,
		Timezone:     tz,
		NotifyChatID: in.NotifyChatID,
		CreatedBy:    actorID,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	secretsOut := map[string]string{}
	cfgRaw := in.TriggerConfig
	if cfgRaw == nil {
		cfgRaw = json.RawMessage(`{}`)
	}

	switch in.TriggerType {
	case TriggerCron:
		if in.EmployeeID == "" || in.Prompt == "" {
			return nil, nil, fmt.Errorf("%w: cron 需要 employee_id 与 prompt", ErrInvalidInput)
		}
		cfg, err := ParseCronConfig(cfgRaw)
		if err != nil {
			return nil, nil, err
		}
		b, _ := json.Marshal(cfg)
		a.TriggerConfig = b
	case TriggerCalendar:
		a.TriggerConfig = json.RawMessage(`{}`)
	case TriggerWebhook:
		if in.EmployeeID == "" || in.Prompt == "" {
			return nil, nil, fmt.Errorf("%w: webhook 需要 employee_id 与 prompt", ErrInvalidInput)
		}
		var cfg WebhookConfig
		_ = json.Unmarshal(cfgRaw, &cfg)
		if cfg.PathToken == "" {
			cfg.PathToken = randomToken(16)
		}
		if len(cfg.AuthModes) == 0 {
			cfg.AuthModes = []string{"hmac", "bearer"}
		}
		if cfg.RateLimitPerMin <= 0 {
			cfg.RateLimitPerMin = 60
		}
		if s.Vault == nil {
			return nil, nil, fmt.Errorf("%w: secret vault 未配置", ErrInvalidInput)
		}
		if hasAuthMode(cfg.AuthModes, "hmac") {
			sec := randomToken(32)
			ref, err := s.Vault.Put("automation.hmac."+a.ID, sec)
			if err != nil {
				return nil, nil, err
			}
			cfg.SecretRef = ref.ID
			secretsOut["hmac_secret"] = sec
		}
		if hasAuthMode(cfg.AuthModes, "bearer") {
			tok := randomToken(32)
			ref, err := s.Vault.Put("automation.bearer."+a.ID, tok)
			if err != nil {
				return nil, nil, err
			}
			cfg.BearerRef = ref.ID
			secretsOut["bearer_token"] = tok
		}
		b, _ := json.Marshal(cfg)
		a.TriggerConfig = b
	}

	if err := s.Store.Save(ctx, a); err != nil {
		return nil, nil, err
	}
	if s.Audit != nil {
		s.Audit.Log(ctx, "USER", actorID, "automation.create", "success", "", map[string]string{
			"id": a.ID, "trigger_type": a.TriggerType, "target_type": "automation", "target_id": a.ID,
		})
	}
	return a, secretsOut, nil
}

// Update 更新规则。
func (s *Service) Update(ctx context.Context, id string, in UpdateInput, actorID string) (*Automation, error) {
	a, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		a.Name = *in.Name
	}
	if in.Enabled != nil {
		a.Enabled = *in.Enabled
	}
	if in.EmployeeID != nil {
		a.EmployeeID = *in.EmployeeID
	}
	if in.Prompt != nil {
		a.Prompt = *in.Prompt
	}
	if in.Timezone != nil {
		a.Timezone = *in.Timezone
	}
	if in.NotifyChatID != nil {
		a.NotifyChatID = *in.NotifyChatID
	}
	if in.TriggerConfig != nil {
		switch a.TriggerType {
		case TriggerCron:
			cfg, err := ParseCronConfig(*in.TriggerConfig)
			if err != nil {
				return nil, err
			}
			b, _ := json.Marshal(cfg)
			a.TriggerConfig = b
		case TriggerWebhook:
			var old, neu WebhookConfig
			_ = json.Unmarshal(a.TriggerConfig, &old)
			_ = json.Unmarshal(*in.TriggerConfig, &neu)
			// 保留 path_token 与 secret refs，除非显式传入
			if neu.PathToken == "" {
				neu.PathToken = old.PathToken
			}
			if neu.SecretRef == "" {
				neu.SecretRef = old.SecretRef
			}
			if neu.BearerRef == "" {
				neu.BearerRef = old.BearerRef
			}
			if len(neu.AuthModes) == 0 {
				neu.AuthModes = old.AuthModes
			}
			if neu.RateLimitPerMin <= 0 {
				neu.RateLimitPerMin = old.RateLimitPerMin
			}
			b, _ := json.Marshal(neu)
			a.TriggerConfig = b
		default:
			a.TriggerConfig = *in.TriggerConfig
		}
	}
	a.UpdatedAt = time.Now().UTC()
	if err := s.Store.Save(ctx, a); err != nil {
		return nil, err
	}
	if s.Audit != nil {
		s.Audit.Log(ctx, "USER", actorID, "automation.update", "success", "", map[string]string{
			"id": a.ID, "target_type": "automation", "target_id": a.ID,
		})
	}
	return a, nil
}

// Delete 删除规则。
func (s *Service) Delete(ctx context.Context, id, actorID string) error {
	if err := s.Store.Delete(ctx, id); err != nil {
		return err
	}
	if s.Audit != nil {
		s.Audit.Log(ctx, "USER", actorID, "automation.delete", "success", "", map[string]string{
			"id": id, "target_type": "automation", "target_id": id,
		})
	}
	return nil
}

// ReplaceCalendarItems 覆盖某日条目（seq 从 1 重排）。
func (s *Service) ReplaceCalendarItems(ctx context.Context, automationID, runDate string, drafts []CalendarItemInput, actorID string) ([]*CalendarItem, error) {
	a, err := s.Store.Get(ctx, automationID)
	if err != nil {
		return nil, err
	}
	if a.TriggerType != TriggerCalendar {
		return nil, fmt.Errorf("%w: 非日历规则", ErrInvalidInput)
	}
	if _, err := time.Parse("2006-01-02", runDate); err != nil {
		return nil, fmt.Errorf("%w: run_date", ErrInvalidInput)
	}
	now := time.Now().UTC()
	items := make([]*CalendarItem, 0, len(drafts))
	for i, d := range drafts {
		if d.EmployeeID == "" || d.Prompt == "" {
			return nil, fmt.Errorf("%w: 日历条目需要 employee_id 与 prompt", ErrInvalidInput)
		}
		en := true
		if d.Enabled != nil {
			en = *d.Enabled
		}
		items = append(items, &CalendarItem{
			ID:           idgen.New("ACI"),
			AutomationID: automationID,
			RunDate:      runDate,
			Seq:          i + 1,
			EmployeeID:   d.EmployeeID,
			Prompt:       d.Prompt,
			Enabled:      en,
			CreatedAt:    now,
			UpdatedAt:    now,
		})
	}
	if err := s.Store.ReplaceCalendarItems(ctx, automationID, runDate, items); err != nil {
		return nil, err
	}
	if s.Audit != nil {
		s.Audit.Log(ctx, "USER", actorID, "automation.calendar_replace", "success", "", map[string]string{
			"id": automationID, "run_date": runDate, "count": fmt.Sprintf("%d", len(items)),
		})
	}
	return items, nil
}

// ListCalendarItems 列某日条目。
func (s *Service) ListCalendarItems(ctx context.Context, automationID, runDate string) ([]*CalendarItem, error) {
	return s.Store.ListCalendarItems(ctx, automationID, runDate)
}

// ListRuns 列出执行记录。
func (s *Service) ListRuns(ctx context.Context, automationID string, limit int) ([]*Run, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.Store.ListRuns(ctx, automationID, limit)
}

// RotateWebhookSecrets 轮换 HMAC / Bearer，明文只返回一次。
func (s *Service) RotateWebhookSecrets(ctx context.Context, id, actorID string) (map[string]string, error) {
	a, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.TriggerType != TriggerWebhook {
		return nil, fmt.Errorf("%w: 非 webhook 规则", ErrInvalidInput)
	}
	var cfg WebhookConfig
	_ = json.Unmarshal(a.TriggerConfig, &cfg)
	out := map[string]string{}
	if s.Vault == nil {
		return nil, fmt.Errorf("%w: vault 未配置", ErrInvalidInput)
	}
	if hasAuthMode(cfg.AuthModes, "hmac") || cfg.SecretRef != "" {
		sec := randomToken(32)
		ref, err := s.Vault.Put("automation.hmac."+a.ID, sec)
		if err != nil {
			return nil, err
		}
		cfg.SecretRef = ref.ID
		out["hmac_secret"] = sec
		if !hasAuthMode(cfg.AuthModes, "hmac") {
			cfg.AuthModes = append(cfg.AuthModes, "hmac")
		}
	}
	if hasAuthMode(cfg.AuthModes, "bearer") || cfg.BearerRef != "" {
		tok := randomToken(32)
		ref, err := s.Vault.Put("automation.bearer."+a.ID, tok)
		if err != nil {
			return nil, err
		}
		cfg.BearerRef = ref.ID
		out["bearer_token"] = tok
		if !hasAuthMode(cfg.AuthModes, "bearer") {
			cfg.AuthModes = append(cfg.AuthModes, "bearer")
		}
	}
	b, _ := json.Marshal(cfg)
	a.TriggerConfig = b
	a.UpdatedAt = time.Now().UTC()
	if err := s.Store.Save(ctx, a); err != nil {
		return nil, err
	}
	if s.Audit != nil {
		s.Audit.Log(ctx, "USER", actorID, "automation.rotate_secrets", "success", "", map[string]string{
			"id": a.ID, "target_type": "automation", "target_id": a.ID,
		})
	}
	return out, nil
}

// Fire 统一触发路径：通知 → 建 Job → 绑 chat → 调度。
func (s *Service) Fire(ctx context.Context, req FireRequest, actorID, ip string) (*Run, *job.Job, error) {
	a := req.Automation
	if a == nil {
		return nil, nil, ErrInvalidInput
	}
	idem := req.IdempotencyKey
	if idem == "" {
		idem = "auto:" + idgen.Raw()
	}
	if existing, err := s.Store.GetRunByIdempotency(ctx, idem); err == nil && existing != nil {
		var j *job.Job
		if existing.JobID != "" && s.Jobs != nil {
			j, _ = s.Jobs.Get(ctx, existing.JobID)
		}
		return existing, j, nil
	}

	empID, prompt := a.EmployeeID, a.Prompt
	calID := ""
	if req.CalendarItem != nil {
		empID = req.CalendarItem.EmployeeID
		prompt = req.CalendarItem.Prompt
		calID = req.CalendarItem.ID
	}
	if empID == "" || prompt == "" {
		return nil, nil, fmt.Errorf("%w: 缺少 employee/prompt", ErrInvalidInput)
	}

	now := time.Now().UTC()
	run := &Run{
		ID:             idgen.New("ARUN"),
		AutomationID:   a.ID,
		CalendarItemID: calID,
		Status:         RunTriggered,
		TriggerSource:  req.TriggerSource,
		IdempotencyKey: idem,
		CreatedAt:      now,
	}
	if req.Payload != nil {
		b, _ := json.Marshal(req.Payload)
		run.Payload = b
	}
	if err := s.Store.SaveRun(ctx, run); err != nil {
		return nil, nil, err
	}

	meta := map[string]string{
		"automation_id": a.ID, "run_id": run.ID, "source": req.TriggerSource,
		"target_type": "automation", "target_id": a.ID,
	}
	s.notifyEvent(ctx, "automation.trigger", "success", actorID, ip, a.NotifyChatID, meta, buildTriggerText(a.Name, req.TriggerSource))

	if s.Jobs == nil {
		run.Status = RunFailed
		run.Error = "job service 未配置"
		fin := time.Now().UTC()
		run.FinishedAt = &fin
		_ = s.Store.SaveRun(ctx, run)
		return run, nil, errors.New(run.Error)
	}

	j, _, err := s.Jobs.Create(ctx, job.CreateInput{
		EmployeeID:     empID,
		Prompt:         prompt,
		IdempotencyKey: idem,
		CreatedBy:      "automation:" + a.ID,
	}, actorID, ip)
	if err != nil {
		run.Status = RunFailed
		run.Error = err.Error()
		fin := time.Now().UTC()
		run.FinishedAt = &fin
		_ = s.Store.SaveRun(ctx, run)
		s.notifyEvent(ctx, "automation.job_created", "failed", actorID, ip, a.NotifyChatID, meta, "【自动化建单失败】"+err.Error())
		return run, nil, err
	}

	run.JobID = j.ID
	run.Status = RunJobCreated
	_ = s.Store.SaveRun(ctx, run)

	if s.Notify != nil && a.NotifyChatID != "" {
		s.Notify.RememberChat(j.ID, a.NotifyChatID)
	}
	meta["job_id"] = j.ID
	meta["employee_id"] = empID
	s.notifyEvent(ctx, "automation.job_created", "success", actorID, ip, a.NotifyChatID, meta, buildJobCreatedText(a.Name, j.ID, empID))

	t := time.Now().UTC()
	a.LastFiredAt = &t
	a.UpdatedAt = t
	_ = s.Store.Save(ctx, a)

	if s.Scheduler != nil {
		_, _ = s.Scheduler.ScheduleJob(ctx, j.ID)
	}
	return run, j, nil
}

// Tick 扫描 cron due 与日历当日首条。
func (s *Service) Tick(ctx context.Context, now time.Time) {
	s.tickCron(ctx, now)
	s.tickCalendar(ctx, now)
}

func (s *Service) tickCron(ctx context.Context, now time.Time) {
	list, err := s.Store.List(ctx, TriggerCron)
	if err != nil {
		return
	}
	for _, a := range list {
		if !a.Enabled {
			continue
		}
		cfg, err := ParseCronConfig(a.TriggerConfig)
		if err != nil {
			continue
		}
		loc := loadLocation(a.Timezone)
		due, err := cronDue(cfg.Expr, loc, a.LastFiredAt, now)
		if err != nil || !due {
			continue
		}
		idem := fmt.Sprintf("cron:%s:%s", a.ID, now.In(loc).Format("200601021504"))
		_, _, _ = s.Fire(ctx, FireRequest{
			Automation: a, TriggerSource: "cron", IdempotencyKey: idem,
		}, "system", "")
	}
}

func (s *Service) tickCalendar(ctx context.Context, now time.Time) {
	list, err := s.Store.List(ctx, TriggerCalendar)
	if err != nil {
		return
	}
	for _, a := range list {
		if !a.Enabled {
			continue
		}
		loc := loadLocation(a.Timezone)
		runDate := now.In(loc).Format("2006-01-02")
		items, err := s.Store.ListCalendarItems(ctx, a.ID, runDate)
		if err != nil || len(items) == 0 {
			continue
		}
		var first *CalendarItem
		for _, it := range items {
			if it.Enabled {
				first = it
				break
			}
		}
		if first == nil {
			continue
		}
		has, _ := s.Store.HasActiveOrSuccessForItem(ctx, first.ID)
		if has {
			continue
		}
		idem := fmt.Sprintf("cal:%s:%s:%d", a.ID, runDate, first.Seq)
		_, _, _ = s.Fire(ctx, FireRequest{
			Automation: a, CalendarItem: first, TriggerSource: "calendar", IdempotencyKey: idem,
		}, "system", "")
	}
}

// OnJobTerminal Job 终态：更新 run、通知、推进日历链。
func (s *Service) OnJobTerminal(ctx context.Context, j *job.Job) {
	if j == nil {
		return
	}
	run, err := s.Store.GetRunByJobID(ctx, j.ID)
	if err != nil || run == nil {
		return
	}
	a, _ := s.Store.Get(ctx, run.AutomationID)
	name := run.AutomationID
	chat := ""
	if a != nil {
		name = a.Name
		chat = a.NotifyChatID
	}

	fin := time.Now().UTC()
	run.FinishedAt = &fin
	chainStopped := false
	action := "automation.job_success"
	result := "success"

	switch j.Status {
	case job.StatusSuccess:
		run.Status = RunSuccess
		_ = s.Store.SaveRun(ctx, run)
		s.notifyEvent(ctx, action, result, "system", "", chat, map[string]string{
			"automation_id": run.AutomationID, "run_id": run.ID, "job_id": j.ID,
		}, buildTerminalText(name, j.ID, j.Status, j.Result, false))
		// 日历链：推进下一条
		if a != nil && a.TriggerType == TriggerCalendar && run.CalendarItemID != "" {
			item, err := s.Store.GetCalendarItem(ctx, run.CalendarItemID)
			if err == nil && item != nil {
				next, _ := s.Store.NextCalendarItem(ctx, a.ID, item.RunDate, item.Seq)
				if next != nil {
					idem := fmt.Sprintf("cal:%s:%s:%d", a.ID, item.RunDate, next.Seq)
					_, _, _ = s.Fire(ctx, FireRequest{
						Automation: a, CalendarItem: next, TriggerSource: "calendar_chain", IdempotencyKey: idem,
					}, "system", "")
				}
			}
		}
	case job.StatusFailed, job.StatusCancelled, job.StatusTimeout:
		run.Status = RunFailed
		run.Error = j.Status + ": " + j.Result
		_ = s.Store.SaveRun(ctx, run)
		if a != nil && a.TriggerType == TriggerCalendar {
			chainStopped = true
		}
		action = "automation.job_failed"
		result = "failed"
		s.notifyEvent(ctx, action, result, "system", "", chat, map[string]string{
			"automation_id": run.AutomationID, "run_id": run.ID, "job_id": j.ID, "status": j.Status,
		}, buildTerminalText(name, j.ID, j.Status, j.Result, chainStopped))
	}
}

// HandleWebhook 处理入站 Webhook。
func (s *Service) HandleWebhook(ctx context.Context, pathToken string, r *http.Request, body []byte) (*Run, *job.Job, error) {
	a, err := s.Store.GetByWebhookPathToken(ctx, pathToken)
	if err != nil {
		return nil, nil, ErrNotFound
	}
	if !a.Enabled {
		return nil, nil, ErrForbiddenWebhook
	}
	var cfg WebhookConfig
	_ = json.Unmarshal(a.TriggerConfig, &cfg)

	ip := clientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
	if !ipAllowed(ip, cfg.IPAllowlist) {
		s.auditWebhookDenied(ctx, a.ID, ip, "ip")
		return nil, nil, ErrForbiddenWebhook
	}
	if !s.limiter.allow(cfg.PathToken, cfg.RateLimitPerMin, time.Now().UTC()) {
		s.auditWebhookDenied(ctx, a.ID, ip, "rate")
		return nil, nil, ErrRateLimited
	}

	needHMAC := hasAuthMode(cfg.AuthModes, "hmac")
	needBearer := hasAuthMode(cfg.AuthModes, "bearer")
	if !needHMAC && !needBearer {
		needHMAC = true
	}

	authOK := false
	if needHMAC {
		sec := ""
		if cfg.SecretRef != "" && s.Vault != nil {
			sec, _ = s.Vault.Get(cfg.SecretRef)
		}
		ts := r.Header.Get("X-AIE-Timestamp")
		sig := r.Header.Get("X-AIE-Signature")
		if err := verifyHMAC(sec, ts, sig, string(body), time.Now().UTC(), 5*time.Minute); err == nil {
			authOK = true
		}
	}
	if needBearer && !authOK {
		tok := ""
		if cfg.BearerRef != "" && s.Vault != nil {
			tok, _ = s.Vault.Get(cfg.BearerRef)
		}
		if err := verifyBearer(tok, r.Header.Get("Authorization")); err == nil {
			authOK = true
		}
	}
	// 若同时配置两种：要求至少一种通过；若只配一种则必须该种通过
	if needHMAC && needBearer {
		// 至少一种
		if !authOK {
			s.auditWebhookDenied(ctx, a.ID, ip, "auth")
			return nil, nil, ErrForbiddenWebhook
		}
	} else if !authOK {
		s.auditWebhookDenied(ctx, a.ID, ip, "auth")
		return nil, nil, ErrForbiddenWebhook
	}

	idem := strings.TrimSpace(r.Header.Get("X-Idempotency-Key"))
	if idem == "" {
		idem = "wh:" + a.ID + ":" + idgen.Raw()
	} else {
		idem = "wh:" + a.ID + ":" + idem
	}
	return s.Fire(ctx, FireRequest{
		Automation: a, TriggerSource: "webhook", IdempotencyKey: idem,
		Payload: map[string]string{"ip": ip},
	}, "webhook", ip)
}

func (s *Service) auditWebhookDenied(ctx context.Context, autoID, ip, reason string) {
	if s.Audit == nil {
		return
	}
	s.Audit.Log(ctx, "SYSTEM", "webhook", "automation.webhook_denied", "failed", ip, map[string]string{
		"automation_id": autoID, "reason": reason,
	})
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return idgen.Raw()
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

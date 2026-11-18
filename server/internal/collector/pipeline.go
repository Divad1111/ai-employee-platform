package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

func changed(a, b map[string]any) []string {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	out := []string{}
	for k := range keys {
		if digest(a[k]) != digest(b[k]) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
func comparableFields(item Item) map[string]any {
	fields := map[string]any{}
	if item.ID == "" {
		return fields
	}
	for k, v := range item.Fields {
		fields[k] = v
	}
	fields["status"], fields["title"], fields["assignee"], fields["due_date"] = item.Status, item.Title, item.Assignees, item.DueDate
	return fields
}
func eventValue(e Event, field string) string {
	switch field {
	case "source":
		return e.Item.Source
	case "event_kind":
		return e.Kind
	case "title":
		return e.Item.Title
	case "status":
		return e.Item.Status
	case "assignee":
		return strings.Join(e.Item.Assignees, ",")
	case "due_date":
		return e.Item.DueDate
	}
	return cellText(e.Item.Fields[field])
}
func matchCondition(c Condition, e Event, p Profile, now time.Time) bool {
	v := eventValue(e, c.Field)
	switch c.Operator {
	case "equals":
		return strings.EqualFold(v, c.Value)
	case "not_equals":
		return !strings.EqualFold(v, c.Value)
	case "contains":
		return strings.Contains(strings.ToLower(v), strings.ToLower(c.Value))
	case "changed":
		return contains(e.ChangedFields, c.Field)
	case "in":
		return matchesAny([]string{v}, stringsOf(c.Value))
	case "is_me":
		return matchesAny(stringsOf(v), append([]string{p.DisplayName, p.FeishuName, p.FeishuOpenID, p.JiraAccount, p.MeegoUserKey}, p.Aliases...))
	case "exists":
		return v != ""
	case "due_within":
		n, err := strconv.Atoi(c.Value)
		if err != nil || n < 0 {
			return false
		}
		d, err := time.Parse("2006-01-02", v)
		loc, errLoc := time.LoadLocation(p.Timezone)
		if errLoc != nil {
			loc = time.UTC
		}
		return err == nil && !e.Item.Done && d.Format("2006-01-02") <= now.In(loc).AddDate(0, 0, n).Format("2006-01-02")
	}
	return false
}
func Evaluate(rules []Rule, e Event, p Profile, now time.Time) (string, string, string, int) {
	rules = append([]Rule(nil), rules...)
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Priority == rules[j].Priority {
			return rules[i].ID < rules[j].ID
		}
		return rules[i].Priority < rules[j].Priority
	})
	for _, r := range rules {
		if !r.Enabled || r.Source != "" && r.Source != e.Item.Source || r.TargetID != "" && r.TargetID != e.Item.TargetID {
			continue
		}
		matches := 0
		for _, c := range r.Conditions {
			if matchCondition(c, e, p, now) {
				matches++
			}
		}
		if len(r.Conditions) == 0 || r.Match == "any" && matches > 0 || r.Match != "any" && matches == len(r.Conditions) {
			return r.Action, r.ID, "命中规则：" + r.Name, r.AggregateMinutes
		}
	}
	return "record", "", "未命中通知规则，仅记录", 0
}

func nextRun(t Target, now time.Time) time.Time {
	if t.Cron != "" {
		if schedule, err := cron.ParseStandard("CRON_TZ=" + t.Timezone + " " + t.Cron); err == nil {
			return schedule.Next(now)
		}
	}
	return now.Add(time.Duration(max(1, t.IntervalMinutes)) * time.Minute)
}

func (s *Service) RequestScan(ctx context.Context, owner, id string) (Run, error) {
	if err := s.active(ctx, owner); err != nil {
		return Run{}, err
	}
	unlock, err := s.lock(ctx, "scan:"+owner+":"+id, 6*time.Minute)
	if err != nil {
		return Run{}, err
	}
	defer unlock()
	target, err := load[Target](ctx, s.Store, owner, "target", id)
	if err != nil {
		return Run{}, err
	}
	if boolean(target.Config, "search_only", false) {
		return Run{}, errors.New("此目录仅用于内容搜索；如需扫描，请先改为变更监控")
	}
	if run, e := load[Run](ctx, s.Store, owner, "request", id); e == nil {
		return run, nil
	} else if !errors.Is(e, ErrNotFound) {
		return Run{}, e
	}
	run := Run{ID: randomID(), TargetID: id, TargetName: target.Name, Source: target.Source, Status: "queued", StartedAt: time.Now().UTC(), ConfigVersion: target.Version, Events: []Event{}, Samples: []Item{}}
	return run, s.Store.Commit(ctx, []Record{record(owner, "request", id, run), record(owner, "run", run.ID, run)})
}

func (s *Service) Scan(ctx context.Context, owner, id string, preview bool, draft *Target) (Run, error) {
	if err := s.active(ctx, owner); err != nil {
		return Run{}, err
	}
	var target Target
	var err error
	if draft != nil && preview {
		target = *draft
		target.OwnerID = owner
		if target.ID == "" {
			target.ID = "preview"
		}
		if err = validateTarget(target); err != nil {
			return Run{}, err
		}
	} else {
		target, err = load[Target](ctx, s.Store, owner, "target", id)
		if err != nil {
			return Run{}, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	unlock, err := s.lock(ctx, "scan:"+owner+":"+target.ID, 6*time.Minute)
	if err != nil {
		return Run{}, err
	}
	defer unlock()
	if !preview {
		target, err = load[Target](ctx, s.Store, owner, "target", id)
		if err != nil {
			return Run{}, err
		}
	}
	now := time.Now().UTC()
	run := Run{ID: randomID(), TargetID: target.ID, TargetName: target.Name, Source: target.Source, Preview: preview, Status: "running", StartedAt: now, ConfigVersion: target.Version, Events: []Event{}, Samples: []Item{}}
	if !preview {
		if requested, e := load[Run](ctx, s.Store, owner, "request", target.ID); e == nil {
			run.ID = requested.ID
			defer s.Store.Delete(context.Background(), owner, "request", target.ID)
		} else if !errors.Is(e, ErrNotFound) {
			return run, e
		}
		if err = s.Store.Commit(ctx, []Record{record(owner, "run", run.ID, run)}); err != nil {
			return run, err
		}
	}
	finishError := func(cause error) (Run, error) {
		run.Status, run.Error, run.FinishedAt = "failed", cause.Error(), time.Now().UTC()
		run.NextAt = now.Add(time.Minute)
		if !preview {
			_ = s.Store.Commit(context.Background(), []Record{record(owner, "run", run.ID, run), record(owner, "schedule", target.ID, run.NextAt)})
		}
		return run, cause
	}
	p, err := s.Profile(ctx, owner)
	if err != nil {
		return finishError(err)
	}
	old, err := load[Snapshot](ctx, s.Store, owner, "snapshot", target.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return finishError(err)
	}
	baseline := errors.Is(err, ErrNotFound)
	rules, err := list[Rule](ctx, s.Store, owner, "rule")
	if err != nil {
		return finishError(err)
	}
	fetched, err := s.fetch(ctx, target, p, old.ScannedAt)
	if err != nil {
		return finishError(err)
	}
	before := map[string]Item{}
	for _, item := range old.Items {
		before[item.ID] = item
	}
	after := map[string]Item{}
	if fetched.Incremental {
		for k, v := range before {
			if target.Source == "feishu_im" && v.UpdatedAt.Before(now.AddDate(0, 0, -max(1, integer(target.Config, "retention_days", 90)))) {
				continue
			}
			after[k] = v
		}
	}
	for _, item := range fetched.Items {
		after[item.ID] = item
	}
	records := []Record{}
	deliveries := map[string]*Delivery{}
	// Serializes batch merge with other target scans and delivery claims.
	if !preview {
		outboxUnlock, e := s.lock(ctx, "outbox:"+owner, 2*time.Minute)
		if e != nil {
			return finishError(e)
		}
		defer outboxUnlock()
	}
	queue := func(e Event, minutes int) error {
		if preview || e.Action == "record" || !p.NotifyEnabled {
			return nil
		}
		key, next := e.ID, now
		if e.Action == "batched" {
			window := int64(max(1, minutes) * 60)
			bucket := now.Unix() / window
			key = digest([]any{"batch", e.RuleID, window, bucket})
			next = time.Unix((bucket+1)*window, 0).UTC()
		}
		d := deliveries[key]
		if d == nil {
			if e.Action == "batched" {
				existing, err := load[Delivery](ctx, s.Store, owner, "delivery", key)
				if err == nil && existing.Status == "pending" && existing.NextAt.After(now) {
					d = &existing
				} else if err != nil && !errors.Is(err, ErrNotFound) {
					return err
				}
			}
			if d == nil {
				d = &Delivery{ID: key, Status: "pending", NextAt: next, Events: []Event{}}
			}
			deliveries[key] = d
		}
		d.Events = append(d.Events, e)
		return nil
	}
	for id, item := range after {
		previous, exists := before[id]
		fields := changed(comparableFields(previous), comparableFields(item))
		if exists && len(fields) == 0 {
			continue
		}
		kind := "updated"
		if !exists {
			kind = "created"
		}
		e := Event{ID: randomID(), Item: item, Kind: kind, ChangedFields: fields, Before: previous.Fields, At: now}
		action, ruleID, reason, minutes := Evaluate(rules, e, p, now)
		if baseline && !target.NotifyBaseline {
			action, reason = "record", "首次扫描建立基线"
		}
		e.Action, e.RuleID, e.Reason = action, ruleID, reason
		run.Events = append(run.Events, e)
		records = append(records, record(owner, "event", e.ID, e))
		if err := queue(e, minutes); err != nil {
			return finishError(err)
		}
	}
	if !fetched.Incremental {
		for id, item := range before {
			if _, ok := after[id]; !ok {
				e := Event{ID: randomID(), Item: item, Kind: "removed", ChangedFields: []string{"removed"}, At: now}
				var minutes int
				e.Action, e.RuleID, e.Reason, minutes = Evaluate(rules, e, p, now)
				run.Events = append(run.Events, e)
				records = append(records, record(owner, "event", e.ID, e))
				if err := queue(e, minutes); err != nil {
					return finishError(err)
				}
			}
		}
	}
	items := make([]Item, 0, len(after))
	for _, item := range after {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	run.Count, run.Changes, run.Status = len(items), len(run.Events), "success"
	run.FinishedAt, run.NextAt = time.Now().UTC(), nextRun(target, now)
	run.Samples = items[:min(10, len(items))]
	if preview {
		return run, nil
	}
	// Snapshot, events, delivery outbox and next schedule commit together.
	records = append(records, record(owner, "snapshot", target.ID, Snapshot{items, now, target.Version}), record(owner, "run", run.ID, run), record(owner, "schedule", target.ID, run.NextAt))
	for _, d := range deliveries {
		records = append(records, record(owner, "delivery", d.ID, d))
	}
	if err = s.Store.Commit(ctx, records); err != nil {
		return finishError(err)
	}
	return run, nil
}

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	sem := make(chan struct{}, 4)
	lastPrune := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Since(lastPrune) > time.Hour {
				if s.Store.Prune(ctx, time.Now().UTC()) == nil {
					lastPrune = time.Now()
				}
			}
			rows, err := s.Store.List(ctx, "*", "target")
			if err != nil {
				continue
			}
			for _, row := range rows {
				var t Target
				if json.Unmarshal(row.Body, &t) != nil || boolean(t.Config, "search_only", false) {
					continue
				}
				if s.active(ctx, t.OwnerID) != nil {
					continue
				}
				_, manual := load[Run](ctx, s.Store, t.OwnerID, "request", t.ID)
				if manual != nil {
					if !t.Enabled || !errors.Is(manual, ErrNotFound) {
						continue
					}
					next, e := load[time.Time](ctx, s.Store, t.OwnerID, "schedule", t.ID)
					if e != nil && !errors.Is(e, ErrNotFound) || e == nil && time.Now().Before(next) {
						continue
					}
				}
				select {
				case sem <- struct{}{}:
					go func(t Target) { defer func() { <-sem }(); _, _ = s.Scan(ctx, t.OwnerID, t.ID, false, nil) }(t)
				default:
				}
			}
			s.Deliver(ctx)
		}
	}
}

func (s *Service) Deliver(ctx context.Context) {
	rows, err := s.Store.List(ctx, "*", "delivery")
	if err != nil {
		return
	}
	for _, row := range rows {
		var d Delivery
		if json.Unmarshal(row.Body, &d) != nil || d.Status == "sent" || d.Status == "cancelled" || d.Status == "failed" || time.Now().Before(d.NextAt) {
			continue
		}
		unlock, err := s.lock(ctx, "delivery:"+row.Owner+":"+d.ID, time.Minute)
		if err != nil {
			continue
		}
		func() {
			defer unlock()
			outboxUnlock, err := s.lock(ctx, "outbox:"+row.Owner, 2*time.Minute)
			if err != nil {
				return
			}
			defer outboxUnlock()
			current, err := load[Delivery](ctx, s.Store, row.Owner, "delivery", d.ID)
			if err != nil || current.Status == "sent" || current.Status == "cancelled" || current.Status == "failed" || time.Now().Before(current.NextAt) {
				return
			}
			d = current
			if s.active(ctx, row.Owner) != nil {
				return
			}
			p, err := s.Profile(ctx, row.Owner)
			if err != nil || !p.NotifyEnabled {
				return
			}
			c, err := s.credentials(ctx, p)
			if err != nil {
				return
			}
			parts := []string{}
			for _, e := range d.Events {
				parts = append(parts, fmt.Sprintf("[%s] %s\n%s\n%s", e.Item.Source, e.Item.Title, e.Reason, e.Item.URL))
			}
			message := strings.Join(parts, "\n\n")
			if c.BotWebhook != "" {
				_, err = s.request(ctx, "POST", c.BotWebhook, "", nil, map[string]any{"msg_type": "text", "content": map[string]string{"text": message}})
			} else if p.NotifyChatID != "" {
				content, _ := json.Marshal(map[string]string{"text": message})
				_, err = s.Feishu(ctx, row.Owner, "user", "POST", "/open-apis/im/v1/messages", map[string][]string{"receive_id_type": {"chat_id"}},
					map[string]any{"receive_id": p.NotifyChatID, "msg_type": "text", "content": string(content), "uuid": digest(d.ID)[:32]})
			} else {
				err = errors.New("未配置通知群或机器人 Webhook")
			}
			d.Attempts++
			if err != nil {
				d.Status, d.Error = "retry", err.Error()
				d.NextAt = time.Now().Add(time.Duration(min(60, 1<<min(d.Attempts, 6))) * time.Minute)
				if d.Attempts >= 8 {
					d.Status = "failed"
				}
			} else {
				d.Status, d.Error = "sent", ""
			}
			_ = s.Store.Commit(ctx, []Record{record(row.Owner, "delivery", d.ID, d)})
		}()
	}
}

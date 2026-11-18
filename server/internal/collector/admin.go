package collector

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func (s *Service) Discover(ctx context.Context, t Target) (any, error) {
	if err := s.active(ctx, t.OwnerID); err != nil {
		return nil, err
	}
	identity := text(t.Config, "identity")
	kind, token, node, err := s.resolveRef(ctx, t.OwnerID, identity, t.URL)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"type": kind, "token": token, "node": node, "identity": valueOr(identity, "user")}
	switch kind {
	case "sheet", "sheets":
		tabs, err := s.sheetTabs(ctx, t.OwnerID, identity, token)
		if err != nil {
			return nil, err
		}
		out["sheets"] = tabs
		if id := text(t.Config, "sheet_id"); id != "" {
			if _, err := segment(id); err != nil {
				return nil, err
			}
			data, err := s.Feishu(ctx, t.OwnerID, identity, "GET", "/open-apis/sheets/v2/spreadsheets/"+token+"/values/"+id+"!A1:AZ20", nil, nil)
			if err != nil {
				return nil, err
			}
			out["sample_rows"] = object(data["valueRange"])["values"]
		}
	case "base", "bitable":
		data, err := s.Feishu(ctx, t.OwnerID, identity, "GET", "/open-apis/bitable/v1/apps/"+token+"/tables", nil, nil)
		if err != nil {
			return nil, err
		}
		out["tables"] = data["items"]
		if table := text(t.Config, "table_id"); table != "" {
			if _, err := segment(table); err != nil {
				return nil, err
			}
			data, err = s.Feishu(ctx, t.OwnerID, identity, "GET", "/open-apis/bitable/v1/apps/"+token+"/tables/"+table+"/fields", nil, nil)
			if err != nil {
				return nil, err
			}
			out["fields"] = data["items"]
		}
	case "doc", "docx":
		if node != nil {
			children, err := s.wikiChildren(ctx, t.OwnerID, identity, text(node, "space_id"), text(node, "node_token"))
			if err != nil {
				return nil, err
			}
			out["children"] = children
		}
	}
	return out, nil
}
func (s *Service) PreviewRule(ctx context.Context, owner string, r Rule) ([]Event, error) {
	profile, err := s.Profile(ctx, owner)
	if err != nil {
		return nil, err
	}
	events, err := list[Event](ctx, s.Store, owner, "event")
	if err != nil {
		return nil, err
	}
	out := []Event{}
	for _, event := range events[:min(100, len(events))] {
		event.Action, event.RuleID, event.Reason, _ = Evaluate([]Rule{r}, event, profile, time.Now())
		out = append(out, event)
	}
	return out, nil
}
func (s *Service) RetryDelivery(ctx context.Context, owner, id string) error {
	unlock, err := s.lock(ctx, "delivery:"+owner+":"+id, time.Minute)
	if err != nil {
		return err
	}
	defer unlock()
	d, err := load[Delivery](ctx, s.Store, owner, "delivery", id)
	if err != nil {
		return err
	}
	if d.Status == "sent" {
		return errors.New("已发送的通知不可重复重试")
	}
	d.Status, d.Error, d.Attempts, d.NextAt = "pending", "", 0, time.Now().UTC()
	return s.Store.Commit(ctx, []Record{record(owner, "delivery", id, d)})
}

// Webhooks are hints to re-read an authorized source. The durable scheduled
// scan prevents an untrusted payload from adding data or choosing recipients.
func (s *Service) Webhook(ctx context.Context, owner, target string, headers http.Header, body io.Reader) error {
	if err := s.active(ctx, owner); err != nil {
		return err
	}
	t, err := load[Target](ctx, s.Store, owner, "target", target)
	if err != nil {
		return err
	}
	if !t.Enabled {
		return errors.New("目标已停用")
	}
	p, err := s.Profile(ctx, owner)
	if err != nil {
		return err
	}
	c, err := s.credentials(ctx, p)
	if err != nil {
		return err
	}
	if c.WebhookSecret == "" {
		return errors.New("签名密钥未配置")
	}
	b, err := io.ReadAll(io.LimitReader(body, (2<<20)+1))
	if err != nil || len(b) > 2<<20 {
		return errors.New("事件过大")
	}
	mac := hmac.New(sha256.New, []byte(c.WebhookSecret))
	mac.Write(b)
	actual, err := hex.DecodeString(strings.TrimPrefix(headers.Get("X-Hub-Signature"), "sha256="))
	if err != nil || !hmac.Equal(actual, mac.Sum(nil)) {
		return errors.New("签名校验失败")
	}
	if ok, err := s.Store.Acquire(ctx, "webhook:"+owner+":"+target+":"+digest(b), randomID(), 24*time.Hour); err != nil {
		return err
	} else if !ok {
		return nil
	}
	return s.Store.Commit(ctx, []Record{record(owner, "schedule", target, time.Now().UTC())})
}

func (s *Service) Import(ctx context.Context, owner, kind, content string, apply bool) (any, error) {
	if len(content) > 2<<20 {
		return nil, errors.New("配置文件超过 2 MiB")
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
		return nil, errors.New("YAML 格式无效")
	}
	targets := []Target{}
	rules := []Rule{}
	warnings := []string{}
	switch kind {
	case "sheets":
		defaults := object(raw["defaults"])
		for _, v := range array(raw["spreadsheets"]) {
			row := object(v)
			cfg := map[string]any{}
			for k, v := range defaults {
				cfg[k] = v
			}
			for _, k := range []string{"person_columns", "notify_fields", "range", "due_column", "status_column", "discover_sheets"} {
				if row[k] != nil {
					cfg[k] = row[k]
				}
			}
			tabs := array(row["sheets"])
			if len(tabs) == 0 {
				tabs = []any{map[string]any{}}
			}
			for _, tab := range tabs {
				tcfg := map[string]any{}
				for k, v := range cfg {
					tcfg[k] = v
				}
				for k, v := range object(tab) {
					if k != "name" {
						tcfg[k] = v
					}
				}
				name := text(row, "name")
				if n := text(object(tab), "name"); n != "" {
					name += " / " + n
				}
				if text(tcfg, "sheet_id") != "" {
					tcfg["sheet_ids"] = []string{text(tcfg, "sheet_id")}
				}
				targets = append(targets, Target{Name: valueOr(name, text(row, "id")), Source: "sheet", URL: text(row, "url"), Enabled: false, IntervalMinutes: 60, Timezone: "Asia/Shanghai", Config: tcfg})
			}
		}
	case "wiki":
		for _, section := range []string{"documents", "knowledge_roots"} {
			for _, v := range array(raw[section]) {
				row := object(v)
				link := text(row, "url")
				if link == "" && text(row, "node_token") != "" {
					link = "https://feishu.cn/wiki/" + text(row, "node_token")
				}
				targets = append(targets, Target{Name: valueOr(text(row, "name"), text(row, "id")), Source: "wiki", URL: link, Enabled: false, IntervalMinutes: 60, Timezone: "Asia/Shanghai",
					Config: map[string]any{"search_only": section == "knowledge_roots", "recursive": section == "knowledge_roots", "max_depth": 3, "max_nodes": 200, "obj_types": []string{"docx", "doc"}}})
			}
		}
	case "rules":
		for i, v := range array(raw["rules"]) {
			row := object(v)
			when := object(row["when"])
			conditions := []Condition{}
			for k := range when {
				switch k {
				case "assignee", "role_is_me", "people", "person_columns", "mentioned_me":
				default:
					return nil, fmt.Errorf("第 %d 条规则包含不能无损转换的条件 %s；请在后台重建该规则，不会忽略此条件后导入", i+1, k)
				}
			}
			if text(when, "assignee") == "me" || boolean(when, "role_is_me", false) {
				conditions = append(conditions, Condition{"assignee", "is_me", ""})
			}
			if value := text(when, "assignee"); value != "" && value != "me" {
				conditions = append(conditions, Condition{"assignee", "contains", value})
			}
			if text(when, "people") == "me" || boolean(when, "mentioned_me", false) {
				conditions = append(conditions, Condition{"assignee", "is_me", ""})
			} else if when["people"] != nil {
				return nil, errors.New("people 条件目前仅支持 me，请在后台配置其他人员规则")
			}
			action := text(row, "notify")
			if action == "" {
				action = "record"
			}
			if action != "instant" && action != "batched" && action != "record" {
				return nil, errors.New("旧规则包含不支持的通知方式")
			}
			rules = append(rules, Rule{Name: "导入规则 " + text(row, "source"), Source: text(row, "source"), Enabled: false, Priority: i + 1, Match: "all", Conditions: conditions, Action: action, AggregateMinutes: 10})
			warnings = append(warnings, "旧规则已转为草稿；请复核 @、人员字段及通知条件后启用")
		}
	case "people":
		people := array(raw["people"])
		if len(people) != 1 {
			return nil, errors.New("请先选择一个人员条目导入到当前用户；不自动创建或匹配平台账号")
		}
		row := object(people[0])
		if !apply {
			return map[string]any{"person": map[string]any{"display_name": text(row, "display_name"), "aliases": stringsOf(row["aliases"]), "jira_account": text(row, "jira_account")},
				"warnings": []string{"仅导入当前用户的身份与凭证；旧 MCP Token 不导入，飞书用户授权需要重新完成"}}, nil
		}
		p, err := s.Profile(ctx, owner)
		if err != nil {
			return nil, err
		}
		p.DisplayName, p.Aliases, p.JiraAccount, p.MeegoUserKey = text(row, "display_name"), stringsOf(row["aliases"]), text(row, "jira_account"), text(row, "meego_user_key")
		err = s.UpdateProfile(ctx, owner, ProfileInput{Profile: p, Credentials: Credentials{JiraToken: text(row, "jira_api_token"), BotWebhook: text(row, "bot_webhook_url")}})
		if err != nil {
			return nil, err
		}
		return map[string]any{"applied": true}, nil
	default:
		return nil, errors.New("支持导入 sheets、wiki、rules、people")
	}
	if apply {
		records := []Record{}
		for i := range targets {
			t := &targets[i]
			if err := validateTarget(*t); err != nil {
				return nil, err
			}
			t.ID = randomID()
			t.OwnerID = owner
			t.Version = 1
			t.UpdatedAt = time.Now().UTC()
			records = append(records, record(owner, "target", t.ID, t))
		}
		for i := range rules {
			r := &rules[i]
			r.ID = randomID()
			records = append(records, record(owner, "rule", r.ID, r))
		}
		if err := s.Store.Commit(ctx, records); err != nil {
			return nil, err
		}
	}
	// Imported configurations start disabled so user credentials can be checked
	// and a baseline captured before any automatic action.
	return map[string]any{"targets": targets, "rules": rules, "warnings": warnings, "applied": apply}, nil
}

func (s *Service) ObserveFeishuEvent(ctx context.Context, chatID, messageID string) {
	rows, err := s.Store.List(ctx, "*", "target")
	if err != nil {
		return
	}
	for _, row := range rows {
		t, err := load[Target](ctx, s.Store, row.Owner, "target", row.ID)
		if err != nil || !t.Enabled || t.Source != "feishu_im" || text(t.Config, "chat_id") != chatID {
			continue
		}
		if s.active(ctx, row.Owner) != nil {
			continue
		}
		_ = s.Store.Commit(ctx, []Record{record(row.Owner, "schedule", t.ID, time.Now().UTC())})
	}
}

// PublicLink ensures that dashboard anchors never execute imported javascript.
func PublicLink(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	return raw
}

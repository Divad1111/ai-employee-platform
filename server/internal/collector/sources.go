package collector

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var rangePattern = regexp.MustCompile(`^[A-Za-z]+[0-9]+:[A-Za-z]+[0-9]+$`)

func segment(s string) (string, error) {
	if !tokenPattern.MatchString(s) || len(s) > 256 {
		return "", errors.New("无效资源 ID")
	}
	return s, nil
}
func sourceRef(raw string) (kind, token string, err error) {
	u, e := url.Parse(raw)
	if e != nil {
		return "", "", errors.New("无效飞书链接")
	}
	host := strings.ToLower(u.Hostname())
	if u.Scheme != "https" || !(host == "feishu.cn" || strings.HasSuffix(host, ".feishu.cn") || host == "larksuite.com" || strings.HasSuffix(host, ".larksuite.com")) {
		return "", "", errors.New("请填写飞书或 Lark 的 HTTPS 链接")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", errors.New("链接缺少文档 token")
	}
	token, err = segment(parts[1])
	return parts[0], token, err
}
func validateTarget(t Target) error {
	switch t.Source {
	case "jira", "sheet", "wiki", "bitable", "meego", "feishu_im":
	default:
		return errors.New("不支持的来源")
	}
	if len(t.Name) > 500 || len(t.URL) > 4096 {
		return errors.New("名称或资源链接过长")
	}
	if t.Source == "sheet" || t.Source == "wiki" || t.Source == "bitable" {
		if t.URL == "" {
			return errors.New("请填写飞书资源链接")
		}
		kind, _, err := sourceRef(t.URL)
		if err != nil {
			return err
		}
		if kind != "wiki" && kind != "sheets" && kind != "docx" && kind != "doc" && kind != "base" {
			return errors.New("不支持的飞书资源类型")
		}
	}
	if t.Source == "jira" && strings.TrimSpace(text(t.Config, "jql")) == "" {
		return errors.New("请填写 Jira JQL")
	}
	if t.Source == "meego" {
		if _, err := segment(text(t.Config, "project_key")); err != nil {
			return errors.New("请填写飞书项目 project_key")
		}
	}
	if t.Source == "feishu_im" {
		if _, err := segment(text(t.Config, "chat_id")); err != nil {
			return errors.New("请填写群 chat_id")
		}
	}
	if t.Source == "wiki" {
		for _, k := range []string{"include", "exclude"} {
			if _, err := regexp.Compile(text(t.Config, k)); err != nil {
				return errors.New("标题筛选正则无效")
			}
		}
		if boolean(t.Config, "search_only", false) && t.Enabled {
			return errors.New("仅搜索目录不能启用定时采集")
		}
	}
	if t.Source == "bitable" {
		if _, err := segment(text(t.Config, "table_id")); err != nil {
			return errors.New("请填写多维表格 table_id")
		}
	}
	if t.Source == "sheet" {
		r := text(t.Config, "range")
		if r != "" && !rangePattern.MatchString(r) {
			return errors.New("读取范围示例：A1:AZ1000")
		}
	}
	if id := text(t.Config, "identity"); id != "" && id != "user" && id != "app" {
		return errors.New("无效访问身份")
	}
	return nil
}

func (s *Service) resolveRef(ctx context.Context, owner, identity, raw string) (string, string, map[string]any, error) {
	kind, token, err := sourceRef(raw)
	if err != nil {
		return "", "", nil, err
	}
	if kind != "wiki" {
		return kind, token, nil, nil
	}
	data, err := s.Feishu(ctx, owner, identity, "GET", "/open-apis/wiki/v2/spaces/get_node", url.Values{"token": {token}}, nil)
	if err != nil {
		return "", "", nil, err
	}
	node := object(data["node"])
	obj, err := segment(text(node, "obj_token"))
	return text(node, "obj_type"), obj, node, err
}

type fetchResult struct {
	Items       []Item
	Incremental bool
}

func (s *Service) fetch(ctx context.Context, t Target, p Profile, since time.Time) (fetchResult, error) {
	switch t.Source {
	case "jira":
		return s.fetchJira(ctx, t, p, since)
	case "sheet":
		items, err := s.fetchSheet(ctx, t, p)
		return fetchResult{Items: items}, err
	case "wiki":
		items, err := s.fetchWiki(ctx, t)
		return fetchResult{Items: items}, err
	case "bitable":
		items, err := s.fetchBitable(ctx, t, p)
		return fetchResult{Items: items}, err
	case "meego":
		return s.fetchMeego(ctx, t, p, since)
	case "feishu_im":
		items, err := s.fetchMessages(ctx, t, p, since)
		return fetchResult{Items: items, Incremental: true}, err
	}
	return fetchResult{}, errors.New("未知采集源")
}
func (s *Service) fetchJira(ctx context.Context, t Target, p Profile, since time.Time) (fetchResult, error) {
	c, err := s.credentials(ctx, p)
	if err != nil {
		return fetchResult{}, err
	}
	if p.JiraURL == "" || c.JiraToken == "" {
		return fetchResult{}, errors.New("请先在个人设置配置 Jira 站点和凭证")
	}
	jql := text(t.Config, "jql")
	// A configured query is authoritative. Incremental queries must retain completed
	// issues; otherwise status transitions out of a filter would remain stale.
	incremental := boolean(t.Config, "incremental", false) && !since.IsZero()
	if incremental {
		if i := strings.LastIndex(strings.ToLower(jql), "order by"); i >= 0 {
			jql = jql[:i]
		}
		loc, err := time.LoadLocation(t.Timezone)
		if err != nil {
			loc = time.UTC
		}
		jql = "(" + jql + ") AND updated >= '" + since.Add(-time.Duration(integer(t.Config, "overlap_minutes", 10))*time.Minute).In(loc).Format("2006-01-02 15:04") + "' ORDER BY updated ASC"
	}
	bearer := c.JiraToken
	headers := map[string]string{}
	if p.JiraAuth == "basic" {
		bearer = ""
		headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(p.JiraAccount+":"+c.JiraToken))
	}
	items := []Item{}
	for start := 0; start < 10000; {
		q := url.Values{"jql": {jql}, "startAt": {strconv.Itoa(start)}, "maxResults": {"100"}}
		data, err := s.request(ctx, http.MethodGet, strings.TrimRight(p.JiraURL, "/")+"/rest/api/2/search?"+q.Encode(), bearer, headers, nil)
		if err != nil {
			return fetchResult{}, err
		}
		rows := array(data["issues"])
		for _, v := range rows {
			r := object(v)
			fields := object(r["fields"])
			id := text(r, "key")
			assignee := object(fields["assignee"])
			names := []string{text(assignee, "name"), text(assignee, "accountId"), text(assignee, "displayName")}
			if boolean(t.Config, "only_mine", true) && !matchesAny(names, append([]string{p.JiraAccount, p.DisplayName}, p.Aliases...)) {
				continue
			}
			status := text(object(fields["status"]), "name")
			items = append(items, Item{ID: t.ID + ":" + id, TargetID: t.ID, Source: t.Source, ExternalID: id, Title: text(fields, "summary"), URL: strings.TrimRight(p.JiraURL, "/") + "/browse/" + id,
				Status: status, Done: isDone(status, t.Config) || text(object(object(fields["status"])["statusCategory"]), "key") == "done", DueDate: dateValueAt(fields["duedate"], p.Timezone), Assignees: names, Fields: fields, UpdatedAt: time.Now().UTC()})
		}
		start += len(rows)
		if start >= integer(data, "total", start) || len(rows) == 0 {
			return fetchResult{items, incremental}, nil
		}
	}
	return fetchResult{}, errors.New("Jira 超过一万条，请缩小 JQL 范围")
}

func (s *Service) sheetTabs(ctx context.Context, owner, identity, token string) ([]map[string]any, error) {
	data, err := s.Feishu(ctx, owner, identity, "GET", "/open-apis/sheets/v3/spreadsheets/"+token+"/sheets/query", nil, nil)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, v := range array(data["sheets"]) {
		out = append(out, object(v))
	}
	return out, nil
}
func (s *Service) fetchSheet(ctx context.Context, t Target, p Profile) ([]Item, error) {
	return s.fetchSheetRows(ctx, t, p, false)
}
func (s *Service) fetchSheetRows(ctx context.Context, t Target, p Profile, contentRead bool) ([]Item, error) {
	identity := text(t.Config, "identity")
	kind, token, _, err := s.resolveRef(ctx, t.OwnerID, identity, t.URL)
	if err != nil {
		return nil, err
	}
	if kind != "sheet" && kind != "sheets" {
		return nil, errors.New("该链接不是电子表格")
	}
	tabs, err := s.sheetTabs(ctx, t.OwnerID, identity, token)
	if err != nil {
		return nil, err
	}
	selected := stringsOf(t.Config["sheet_ids"])
	if len(selected) == 0 && text(t.Config, "sheet_id") != "" {
		selected = []string{text(t.Config, "sheet_id")}
	}
	if len(selected) == 0 {
		u, _ := url.Parse(t.URL)
		if tab := u.Query().Get("sheet"); tab != "" {
			selected = []string{tab}
		}
	}
	if boolean(t.Config, "discover_sheets", false) {
		selected = nil
	}
	if len(selected) == 0 && !boolean(t.Config, "discover_sheets", false) {
		return nil, errors.New("请选择工作表，或开启自动发现工作表")
	}
	items := []Item{}
	seen := map[string]bool{}
	found := map[string]bool{}
	for _, tab := range tabs {
		id, err := segment(text(tab, "sheet_id"))
		if err != nil {
			return nil, err
		}
		if len(selected) > 0 && !contains(selected, id) {
			continue
		}
		found[id] = true
		region := text(t.Config, "range")
		if region == "" {
			region = "A1:AZ1000"
		}
		data, err := s.Feishu(ctx, t.OwnerID, identity, "GET", "/open-apis/sheets/v2/spreadsheets/"+token+"/values/"+id+"!"+region, url.Values{"valueRenderOption": {"ToString"}, "dateTimeRenderOption": {"FormattedString"}}, nil)
		if err != nil {
			return nil, err
		}
		rows := array(object(data["valueRange"])["values"])
		header := integer(t.Config, "header_row", 1) - 1
		if header < 0 || header >= len(rows) {
			return nil, errors.New("表头行超出读取范围")
		}
		headers := array(rows[header])
		labels := []string{}
		for _, v := range headers {
			label := cellText(v)
			if contentRead && label == "" {
				label = fmt.Sprintf("列%d", len(labels)+1)
			}
			labels = append(labels, label)
		}
		personCols := stringsOf(t.Config["person_columns"])
		if len(personCols) == 0 {
			personCols = []string{"负责人", "开发人", "验收人"}
		}
		if boolean(t.Config, "only_mine", true) {
			has := false
			for _, col := range personCols {
				if contains(labels, col) {
					has = true
				}
			}
			if !has {
				return nil, errors.New("工作表缺少配置的人员列，请检查表头与字段映射")
			}
		}
		idCol := valueOr(text(t.Config, "id_column"), "单号")
		titleCol := valueOr(text(t.Config, "title_column"), "标题")
		statusCol := valueOr(text(t.Config, "status_column"), "状态")
		dueCol := valueOr(text(t.Config, "due_column"), "截止日期")
		for rowIndex, row := range rows[header+1:] {
			values := array(row)
			fields := map[string]any{}
			nonempty := false
			for i, label := range labels {
				if label != "" && i < len(values) {
					fields[label] = cellText(values[i])
					nonempty = nonempty || cellText(values[i]) != ""
				}
			}
			if !nonempty {
				continue
			}
			people := []string{}
			for _, col := range personCols {
				people = append(people, stringsOf(fields[col])...)
			}
			if boolean(t.Config, "only_mine", true) && !matchesAny(people, append([]string{p.DisplayName, p.FeishuName, p.JiraAccount}, p.Aliases...)) {
				continue
			}
			title := text(fields, titleCol)
			if title == "" {
				title = valueOr(text(fields, "任务"), text(fields, "需求"))
			}
			external := text(fields, idCol)
			if contentRead {
				external = strconv.Itoa(rowIndex + header + 2)
			}
			if external == "" {
				external = digest([]any{title, people})[:24]
			}
			itemID := t.ID + ":" + id + ":" + external
			if seen[itemID] {
				return nil, fmt.Errorf("工作表 %s 存在重复标识，请配置唯一标识列（数据行 %d）", text(tab, "title"), rowIndex+header+2)
			}
			seen[itemID] = true
			status := text(fields, statusCol)
			items = append(items, Item{ID: itemID, TargetID: t.ID, Source: "sheet", ExternalID: external, Title: title, URL: "https://feishu.cn/sheets/" + token + "?sheet=" + id,
				Status: status, Done: isDone(status, t.Config), DueDate: dateValueAt(fields[dueCol], p.Timezone), Assignees: people, Fields: fields, UpdatedAt: time.Now().UTC()})
		}
	}
	for _, id := range selected {
		if !found[id] {
			return nil, fmt.Errorf("工作表 %s 不存在或不可访问", id)
		}
	}
	return items, nil
}
func (s *Service) fetchBitable(ctx context.Context, t Target, p Profile) ([]Item, error) {
	kind, token, _, err := s.resolveRef(ctx, t.OwnerID, text(t.Config, "identity"), t.URL)
	if err != nil {
		return nil, err
	}
	if kind != "base" && kind != "bitable" {
		return nil, errors.New("该链接不是多维表格")
	}
	table, _ := segment(text(t.Config, "table_id"))
	items := []Item{}
	next := ""
	for page := 0; page < 100; page++ {
		q := url.Values{"page_size": {"100"}, "user_id_type": {"open_id"}}
		if next != "" {
			q.Set("page_token", next)
		}
		body := map[string]any{}
		if view := text(t.Config, "view_id"); view != "" {
			body["view_id"] = view
		}
		if filter, ok := t.Config["filter"].(map[string]any); ok {
			body["filter"] = filter
		}
		data, err := s.Feishu(ctx, t.OwnerID, text(t.Config, "identity"), "POST", "/open-apis/bitable/v1/apps/"+token+"/tables/"+table+"/records/search", q, body)
		if err != nil {
			return nil, err
		}
		for _, v := range array(data["items"]) {
			r := object(v)
			fields := object(r["fields"])
			id := text(r, "record_id")
			people := stringsOf(fields[valueOr(text(t.Config, "person_field"), "负责人")])
			if boolean(t.Config, "only_mine", true) && !matchesAny(people, append([]string{p.FeishuOpenID, p.DisplayName, p.FeishuName}, p.Aliases...)) {
				continue
			}
			status := cellText(fields[valueOr(text(t.Config, "status_column"), "状态")])
			items = append(items, Item{ID: t.ID + ":" + id, TargetID: t.ID, Source: t.Source, ExternalID: id, Title: cellText(fields[valueOr(text(t.Config, "title_column"), "标题")]), URL: t.URL + "?table=" + table + "&record=" + id,
				Status: status, Done: isDone(status, t.Config), DueDate: dateValueAt(fields[valueOr(text(t.Config, "due_column"), "截止日期")], p.Timezone), Assignees: people, Fields: fields, UpdatedAt: time.Now().UTC()})
		}
		if !boolean(data, "has_more", false) {
			return items, nil
		}
		n := text(data, "page_token")
		if n == "" || n == next {
			return nil, errors.New("多维表格分页游标异常")
		}
		next = n
	}
	return nil, errors.New("多维表格超过一万条，请配置筛选条件")
}
func (s *Service) fetchMeego(ctx context.Context, t Target, p Profile, since time.Time) (fetchResult, error) {
	token, err := s.meegoToken(ctx, p.UserID)
	if err != nil {
		return fetchResult{}, err
	}
	if boolean(t.Config, "only_mine", true) && p.MeegoUserKey == "" {
		return fetchResult{}, errors.New("仅采集本人事项需要配置飞书项目 User Key")
	}
	base := valueOr(p.MeegoURL, "https://project.feishu.cn")
	project := text(t.Config, "project_key")
	items := []Item{}
	if since.IsZero() {
		since = time.Now().AddDate(0, 0, -max(1, min(3650, integer(t.Config, "initial_days", 90))))
	}
	for page := 1; page <= 100; page++ {
		body := map[string]any{"page_num": page, "page_size": 100, "updated_at": map[string]any{"start": since.Add(-10 * time.Minute).UnixMilli(), "end": time.Now().UnixMilli()}}
		if p.MeegoUserKey != "" && boolean(t.Config, "only_mine", true) {
			body["user_keys"] = []string{p.MeegoUserKey}
		}
		data, err := s.request(ctx, "POST", strings.TrimRight(base, "/")+"/open_api/"+project+"/work_item/filter", "", map[string]string{"X-PLUGIN-TOKEN": token, "X-USER-KEY": p.MeegoUserKey}, body)
		if err != nil {
			return fetchResult{}, err
		}
		rows := array(data["data"])
		if rows == nil {
			rows = array(object(data["data"])["items"])
		}
		for _, v := range rows {
			r := object(v)
			id := text(r, "id")
			status := text(r, "status")
			items = append(items, Item{ID: t.ID + ":" + id, TargetID: t.ID, Source: "meego", ExternalID: id, Title: text(r, "name"), URL: text(r, "url"), Status: status, Done: isDone(status, t.Config), DueDate: dateValueAt(r["due_date"], p.Timezone), Fields: r, Assignees: []string{p.MeegoUserKey}, UpdatedAt: time.Now().UTC()})
		}
		if len(rows) < 100 {
			return fetchResult{items, true}, nil
		}
	}
	return fetchResult{}, errors.New("飞书项目返回数据过多，请缩小扫描范围")
}

func (s *Service) fetchMessages(ctx context.Context, t Target, p Profile, since time.Time) ([]Item, error) {
	chat, err := segment(text(t.Config, "chat_id"))
	if err != nil {
		return nil, errors.New("请配置群 chat_id")
	}
	if since.IsZero() {
		since = time.Now().AddDate(0, 0, -max(1, min(90, integer(t.Config, "initial_days", 7))))
	}
	q := url.Values{"container_id_type": {"chat"}, "container_id": {chat}, "page_size": {"50"}, "sort_type": {"ByCreateTimeAsc"},
		"start_time": {strconv.FormatInt(since.Add(-10*time.Minute).Unix(), 10)}, "end_time": {strconv.FormatInt(time.Now().Unix(), 10)}}
	items := []Item{}
	for page := 0; page < 200; page++ {
		data, err := s.Feishu(ctx, t.OwnerID, text(t.Config, "identity"), "GET", "/open-apis/im/v1/messages", q, nil)
		if err != nil {
			return nil, err
		}
		for _, v := range array(data["items"]) {
			m := object(v)
			matched := !boolean(t.Config, "only_mentioned", true)
			for _, a := range array(m["mentions"]) {
				mention := object(a)
				if text(mention, "id") == p.FeishuOpenID || text(object(mention["id"]), "open_id") == p.FeishuOpenID {
					matched = true
				}
			}
			if !matched {
				continue
			}
			id := text(m, "message_id")
			content := text(object(m["body"]), "content")
			items = append(items, Item{ID: t.ID + ":" + id, TargetID: t.ID, Source: "feishu_im", ExternalID: id, Title: content, Content: content, Fields: m, Assignees: []string{p.FeishuOpenID}, UpdatedAt: time.Now().UTC()})
		}
		if !boolean(data, "has_more", false) {
			return items, nil
		}
		next := text(data, "page_token")
		if next == "" || next == q.Get("page_token") {
			return nil, errors.New("消息分页游标异常")
		}
		q.Set("page_token", next)
	}
	return nil, errors.New("消息超过一万条，请缩小初始回溯范围")
}

func valueOr(v, def string) string {
	if v != "" {
		return v
	}
	return def
}
func contains(a []string, v string) bool {
	for _, s := range a {
		if s == v {
			return true
		}
	}
	return false
}
func matchesAny(values, names []string) bool {
	for _, v := range values {
		for _, name := range names {
			if strings.TrimSpace(name) != "" && strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(name)) {
				return true
			}
		}
	}
	return false
}
func cellText(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if a, ok := v.([]any); ok {
		parts := []string{}
		for _, r := range a {
			parts = append(parts, cellText(r))
		}
		return strings.Join(parts, ",")
	}
	if m, ok := v.(map[string]any); ok {
		for _, k := range []string{"text", "name", "value", "link"} {
			if m[k] != nil {
				return cellText(m[k])
			}
		}
		return ""
	}
	return fmt.Sprint(v)
}
func isDone(status string, cfg map[string]any) bool {
	states := stringsOf(cfg["done_statuses"])
	if len(states) == 0 {
		states = []string{"done", "closed", "resolved", "完成", "已完成", "已关闭", "已解决", "取消", "已取消"}
	}
	return matchesAny([]string{status}, states)
}
func dateValue(v any) string {
	return dateValueAt(v, "UTC")
}
func dateValueAt(v any, timezone string) string {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	raw := cellText(v)
	if n, err := strconv.ParseFloat(raw, 64); err == nil {
		if n > 1e12 {
			return time.UnixMilli(int64(n)).In(loc).Format("2006-01-02")
		}
		if n > 1e9 {
			return time.Unix(int64(n), 0).In(loc).Format("2006-01-02")
		}
		if n > 20000 && n < 100000 {
			return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(n)).Format("2006-01-02")
		}
	}
	for _, layout := range []string{"2006-01-02", "2006/1/2", "2006-1-2", "2006.1.2", time.RFC3339, "2006-01-02 15:04:05"} {
		if d, e := time.Parse(layout, raw); e == nil {
			return d.Format("2006-01-02")
		}
	}
	return ""
}

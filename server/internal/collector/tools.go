package collector

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ai-employee-platform/server/internal/mcpauth"
	"github.com/ai-employee-platform/server/internal/mcpserver"
)

//go:embed tools.json
var toolJSON []byte

type Provider struct{ Service *Service }

func (p *Provider) Name() string { return mcpauth.Collector }
func (p *Provider) Tools() []map[string]any {
	var defs []map[string]any
	_ = json.Unmarshal(toolJSON, &defs)
	// These user-identity tools extend the old collector with the common local
	// skill workflows: find -> read -> edit/create, with upstream ACL enforcement.
	add := func(name, desc string, write bool, required []string, props map[string]any) {
		if required == nil {
			required = []string{}
		}
		props["identity"] = map[string]any{"type": "string", "enum": []string{"user", "app"}, "description": "默认 user，本人的飞书身份"}
		if write {
			props["confirm_token"] = map[string]any{"type": "string"}
		}
		defs = append(defs, map[string]any{"name": name, "description": desc, "inputSchema": map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}, "annotations": map[string]any{"readOnlyHint": !write}})
	}
	str := func() map[string]any { return map[string]any{"type": "string"} }
	add("search_feishu_documents", "按本人权限搜索云文档；返回原文链接与分页游标", false, []string{"query"}, map[string]any{"query": str(), "page_token": str()})
	add("list_feishu_drive_files", "列出本人云空间目录", false, nil, map[string]any{"folder_token": str(), "page_token": str()})
	add("read_feishu_docx", "读取云文档正文，支持 docx/wiki 链接", false, []string{"url"}, map[string]any{"url": str()})
	add("create_feishu_docx", "以当前身份创建云文档，需确认", true, []string{"title"}, map[string]any{"title": str(), "folder_token": str()})
	add("update_feishu_docx_block", "修改指定文档块；需提供飞书 block 更新 JSON 并确认", true, []string{"document_id", "block_id", "update_json"}, map[string]any{"document_id": str(), "block_id": str(), "update_json": str()})
	add("list_feishu_calendar_events", "读取本人日历的日程，未指定日历时使用本人主日历", false, nil, map[string]any{"calendar_id": str(), "start_time": str(), "end_time": str(), "page_token": str()})
	return defs
}

type pendingWrite struct {
	Principal string         `json:"principal"`
	Tool      string         `json:"tool"`
	Args      map[string]any `json:"args"`
	Identity  string         `json:"identity"`
	Expires   time.Time      `json:"expires_at"`
}

func (p *Provider) Call(ctx context.Context, c *mcpserver.Caller, name string, args map[string]any) (result any, err error) {
	if c == nil || c.UserID == "" {
		return nil, errors.New("缺少平台用户身份")
	}
	s := p.Service
	if err = s.active(ctx, c.UserID); err != nil {
		return nil, err
	}
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		}
		entry := map[string]any{"tool": name, "status": status, "at": time.Now().UTC(), "subject_type": c.Kind}
		_ = s.Store.Commit(ctx, []Record{record(c.UserID, "call", randomID(), entry)})
		if s.Audit != nil {
			s.Audit(ctx, c.UserID, name, status)
		}
	}()
	if args == nil {
		args = map[string]any{}
	}
	var def map[string]any
	for _, d := range p.Tools() {
		if d["name"] == name {
			def = d
			break
		}
	}
	if def == nil {
		return nil, errors.New("未知工具")
	}
	write := !boolean(object(def["annotations"]), "readOnlyHint", true)
	if write {
		if strings.HasPrefix(name, "delete_") {
			if !c.CanDelete {
				return nil, errors.New("删除操作需要独立删除权限")
			}
		} else if !c.CanWrite {
			return nil, errors.New("当前 MCP 凭证没有写权限")
		}
		principal := c.Kind + ":" + c.SubjectID + ":" + c.TokenID
		profile, e := s.Profile(ctx, c.UserID)
		if e != nil {
			return nil, e
		}
		identity := digest([]string{profile.FeishuAppID, profile.FeishuOpenID, profile.GrantID, text(args, "identity")})
		if token := text(args, "confirm_token"); token != "" {
			pending, e := load[pendingWrite](ctx, s.Store, c.UserID, "confirmation", digest(token))
			if e != nil || pending.Principal != principal || pending.Tool != name || time.Now().After(pending.Expires) {
				return nil, errors.New("确认凭证无效、已使用或已过期")
			}
			identity = digest([]string{profile.FeishuAppID, profile.FeishuOpenID, profile.GrantID, text(pending.Args, "identity")})
			if identity != pending.Identity {
				return nil, errors.New("飞书身份已变更，请重新预览")
			}
			supplied := map[string]any{}
			for k, v := range args {
				if k != "confirm_token" {
					supplied[k] = v
				}
			}
			if len(supplied) > 0 && digest(supplied) != digest(pending.Args) {
				return nil, errors.New("操作参数已变更，请重新预览")
			}
			if _, e = s.Store.Take(ctx, c.UserID, "confirmation", digest(token)); e != nil {
				return nil, errors.New("确认凭证已使用")
			}
			args = pending.Args
		} else {
			if err = validateArgs(def, args); err != nil {
				return nil, err
			}
			clean := map[string]any{}
			for k, v := range args {
				if k != "confirm_token" {
					clean[k] = v
				}
			}
			token := randomID()
			pending := pendingWrite{principal, name, clean, identity, time.Now().Add(5 * time.Minute)}
			if err = s.Store.Commit(ctx, []Record{record(c.UserID, "confirmation", digest(token), pending)}); err != nil {
				return nil, err
			}
			preview := map[string]any{}
			for k, v := range clean {
				if k == "file_base64" {
					preview[k] = "[文件内容已接收]"
				} else {
					preview[k] = v
				}
			}
			return map[string]any{"status": "confirmation_required", "confirm_token": token, "expires_in_seconds": 300,
				"preview": map[string]any{"action": name, "identity": valueOr(text(args, "identity"), "user"), "arguments": preview},
				"message": "请向用户展示预览，取得确认后携带 confirm_token 执行；参数改变需重新预览"}, nil
		}
	} else if err = validateArgs(def, args); err != nil {
		return nil, err
	}
	out, e := s.execute(ctx, c, name, args)
	if e != nil {
		return nil, e
	}
	return out, nil
}
func validateArgs(def, args map[string]any) error {
	schema := object(def["inputSchema"])
	props := object(schema["properties"])
	for _, k := range stringsOf(schema["required"]) {
		if args[k] == nil || args[k] == "" {
			return fmt.Errorf("缺少参数 %s", k)
		}
	}
	for k, v := range args {
		field, ok := props[k]
		if !ok {
			return fmt.Errorf("不支持参数 %s", k)
		}
		typ := text(object(field), "type")
		switch typ {
		case "string":
			if _, ok := v.(string); !ok {
				return fmt.Errorf("%s 必须为字符串", k)
			}
		case "boolean":
			if _, ok := v.(bool); !ok {
				return fmt.Errorf("%s 必须为布尔值", k)
			}
		case "integer", "number":
			switch v.(type) {
			case int, float64:
			default:
				return fmt.Errorf("%s 必须为数字", k)
			}
		}
		if enum := stringsOf(object(field)["enum"]); len(enum) > 0 && !contains(enum, fmt.Sprint(v)) {
			return fmt.Errorf("%s 的取值不受支持", k)
		}
	}
	return nil
}

func (s *Service) execute(ctx context.Context, c *mcpserver.Caller, name string, a map[string]any) (any, error) {
	owner, identity := c.UserID, text(a, "identity")
	f := func(method, path string, q url.Values, body any) (map[string]any, error) {
		return s.Feishu(ctx, owner, identity, method, path, q, body)
	}
	q := url.Values{}
	if t := text(a, "page_token"); t != "" {
		q.Set("page_token", t)
	}
	switch name {
	case "whoami":
		return s.ProfileStatus(ctx, owner)
	case "list_people":
		p, e := s.ProfileStatus(ctx, owner)
		return map[string]any{"count": 1, "people": []any{p}, "visibility": "当前身份；其他人员由后台管理员管理"}, e
	case "get_collector_status":
		profile, e := s.ProfileStatus(ctx, owner)
		if e != nil {
			return nil, e
		}
		targets, e := list[Target](ctx, s.Store, owner, "target")
		if e != nil {
			return nil, e
		}
		runs, e := list[Run](ctx, s.Store, owner, "run")
		if e != nil {
			return nil, e
		}
		return map[string]any{"identity": profile, "targets": targets, "runs": runs[:min(20, len(runs))]}, nil
	case "list_api_access_logs":
		if who := text(a, "who"); who != "" && who != owner {
			return nil, errors.New("只能查看本人调用记录")
		}
		rows, e := s.Store.List(ctx, owner, "call")
		if e != nil {
			return nil, e
		}
		profile, e := s.Profile(ctx, owner)
		if e != nil {
			return nil, e
		}
		loc, e := time.LoadLocation(profile.Timezone)
		if e != nil {
			loc = time.UTC
		}
		day := valueOr(text(a, "day"), time.Now().In(loc).Format("2006-01-02"))
		if _, e := time.Parse("2006-01-02", day); e != nil {
			return nil, errors.New("day 应为 YYYY-MM-DD")
		}
		filtered := []Record{}
		for _, row := range rows {
			if row.Updated.In(loc).Format("2006-01-02") == day {
				filtered = append(filtered, row)
			}
			if len(filtered) >= min(500, max(1, integer(a, "limit", 100))) {
				break
			}
		}
		return map[string]any{"items": filtered, "day": day, "visibility": "始终仅返回本人审计记录"}, nil
	case "list_recent_events":
		events, e := list[Event](ctx, s.Store, owner, "event")
		if e != nil {
			return nil, e
		}
		out := []Event{}
		for _, event := range events {
			if source := text(a, "source"); source != "" && source != event.Item.Source {
				continue
			}
			if since := text(a, "since"); since != "" && event.At.Format(time.RFC3339) < since {
				continue
			}
			out = append(out, event)
			if len(out) >= min(500, max(1, integer(a, "limit", 50))) {
				break
			}
		}
		return map[string]any{"count": len(out), "events": out}, nil
	case "get_my_workload", "list_due_soon", "get_daily_brief":
		profile, e := s.Profile(ctx, owner)
		if e != nil {
			return nil, e
		}
		items, e := s.Items(ctx, owner)
		if e != nil {
			return nil, e
		}
		loc, e := time.LoadLocation(profile.Timezone)
		if e != nil {
			loc = time.UTC
		}
		today := time.Now().In(loc)
		date := today.Format("2006-01-02")
		days := integer(a, "days", 7)
		if days < 0 {
			days = 7
		}
		horizon := today.AddDate(0, 0, min(3650, days)).Format("2006-01-02")
		out, overdue, due := []Item{}, []Item{}, []Item{}
		for _, item := range items {
			if item.Done || item.Source == "wiki" || item.Source == "feishu_im" {
				continue
			}
			if item.DueDate != "" && item.DueDate < date {
				overdue = append(overdue, item)
			}
			if item.DueDate == date {
				due = append(due, item)
			}
			switch text(a, "scope") {
			case "today":
				if item.DueDate != date {
					continue
				}
			case "overdue":
				if item.DueDate == "" || item.DueDate >= date {
					continue
				}
			case "this_week":
				days := (7 - int(today.Weekday())) % 7
				if item.DueDate == "" || item.DueDate > today.AddDate(0, 0, days).Format("2006-01-02") {
					continue
				}
			}
			if name == "list_due_soon" && (item.DueDate == "" || item.DueDate > horizon) {
				continue
			}
			out = append(out, item)
		}
		if name == "get_daily_brief" {
			events, e := list[Event](ctx, s.Store, owner, "event")
			if e != nil {
				return nil, e
			}
			recent := []Event{}
			for _, ev := range events {
				yesterday := time.Date(today.Year(), today.Month(), today.Day()-1, 0, 0, 0, 0, loc)
				if !ev.At.Before(yesterday) {
					recent = append(recent, ev)
				}
			}
			return map[string]any{"date": date, "overdue": overdue, "due_today": due, "recent_events": recent[:min(50, len(recent))]}, nil
		}
		return map[string]any{"count": len(out), "tasks": out, "scope": valueOr(text(a, "scope"), "all"), "data_source": "个人采集快照"}, nil
	case "list_sheet_targets", "list_feishu_wiki_docs":
		targets, e := list[Target](ctx, s.Store, owner, "target")
		if e != nil {
			return nil, e
		}
		out := []Target{}
		documents := []map[string]any{}
		source := "sheet"
		if name == "list_feishu_wiki_docs" {
			source = "wiki"
		}
		for _, t := range targets {
			if t.Source != source {
				continue
			}
			if name == "list_feishu_wiki_docs" {
				doc := map[string]any{"target_id": t.ID, "title": t.Name, "url": t.URL, "search_only": boolean(t.Config, "search_only", false)}
				if boolean(a, "refresh", true) {
					kind, token, node, err := s.resolveRef(ctx, owner, identity, t.URL)
					if err != nil {
						return nil, err
					}
					doc["obj_type"], doc["obj_token"], doc["node"] = kind, token, node
					if title := text(node, "title"); title != "" {
						doc["title"] = title
					}
				}
				needle := strings.ToLower(text(a, "name"))
				if needle == "" || strings.Contains(strings.ToLower(t.Name+" "+text(doc, "title")+" "+t.URL), needle) {
					documents = append(documents, doc)
					out = append(out, t)
				}
			} else {
				out = append(out, t)
			}
		}
		if name == "list_feishu_wiki_docs" {
			return map[string]any{"count": len(documents), "documents": documents, "targets": out}, nil
		}
		return map[string]any{"count": len(out), "targets": out}, nil
	case "list_sheet_rows":
		if boolean(a, "refresh", false) || !boolean(a, "only_mine", true) {
			profile, e := s.Profile(ctx, owner)
			if e != nil {
				return nil, e
			}
			targets, e := list[Target](ctx, s.Store, owner, "target")
			if e != nil {
				return nil, e
			}
			rows := []Item{}
			for _, target := range targets {
				if target.Source != "sheet" {
					continue
				}
				target.Config["only_mine"] = boolean(a, "only_mine", true)
				target.Config["identity"] = identity
				if id := text(a, "sheet_id"); id != "" {
					configured := stringsOf(target.Config["sheet_ids"])
					if len(configured) > 0 && !contains(configured, id) && !boolean(target.Config, "discover_sheets", false) {
						continue
					}
					target.Config["sheet_ids"] = []string{id}
					target.Config["discover_sheets"] = false
				}
				current, e := s.fetchSheet(ctx, target, profile)
				if e != nil {
					return nil, e
				}
				rows = append(rows, current...)
			}
			return map[string]any{"count": len(rows), "rows": rows, "cached": false}, nil
		}
		items, e := s.Items(ctx, owner)
		if e != nil {
			return nil, e
		}
		out := []Item{}
		profile, err := s.Profile(ctx, owner)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if item.Source == "sheet" && (text(a, "sheet_id") == "" || strings.Contains(item.ID, ":"+text(a, "sheet_id")+":")) {
				out = append(out, item)
			}
		}
		out = mySheetItems(out, profile)
		return map[string]any{"count": len(out), "rows": out, "cached": true}, nil
	case "get_feishu_wiki_node":
		_, _, node, e := s.resolveRef(ctx, owner, identity, text(a, "url"))
		return node, e
	case "list_feishu_wiki_children":
		space, node := text(a, "space_id"), text(a, "parent_node_token")
		if raw := text(a, "url"); raw != "" {
			_, _, n, e := s.resolveRef(ctx, owner, identity, raw)
			if e != nil {
				return nil, e
			}
			space, node = text(n, "space_id"), text(n, "node_token")
		}
		children, e := s.wikiChildren(ctx, owner, identity, space, node)
		return map[string]any{"items": children}, e
	case "discover_feishu_wiki_tree":
		urls := []string{text(a, "url")}
		if urls[0] == "" {
			urls = nil
			targets, e := list[Target](ctx, s.Store, owner, "target")
			if e != nil {
				return nil, e
			}
			for _, t := range targets {
				if t.Source == "wiki" {
					urls = append(urls, t.URL)
				}
			}
		}
		nodes := []map[string]any{}
		limit := min(1000, max(1, integer(a, "max_nodes", 100)))
		for _, raw := range urls {
			if len(nodes) >= limit {
				break
			}
			found, e := s.wikiTree(ctx, owner, identity, raw, true, 3, limit-len(nodes))
			if e != nil {
				return nil, e
			}
			nodes = append(nodes, found...)
		}
		types := stringsOf(a["obj_types"])
		if len(types) > 0 {
			filtered := []map[string]any{}
			for _, n := range nodes {
				if contains(types, text(n, "obj_type")) {
					filtered = append(filtered, n)
				}
			}
			nodes = filtered
		}
		return map[string]any{"nodes": nodes}, nil
	case "search_feishu_wiki":
		return s.searchWikiTool(ctx, owner, identity, a)
	case "read_feishu_wiki_doc", "read_feishu_docx":
		raw := text(a, "url")
		if raw == "" {
			targets, e := list[Target](ctx, s.Store, owner, "target")
			if e != nil {
				return nil, e
			}
			for _, t := range targets {
				if t.Source == "wiki" && text(a, "name") != "" && (t.ID == text(a, "name") || strings.Contains(strings.ToLower(t.Name), strings.ToLower(text(a, "name")))) {
					raw = t.URL
					break
				}
			}
		}
		kind, token, _, e := s.resolveRef(ctx, owner, identity, raw)
		if e != nil {
			return nil, e
		}
		content, e := s.readDocument(ctx, owner, identity, kind, token)
		if e != nil {
			return nil, e
		}
		out := map[string]any{"content": content, "url": raw, "identity": valueOr(identity, "user")}
		if kind == "docx" && name == "read_feishu_wiki_doc" && boolean(a, "include_whiteboards", true) {
			boards, e := s.readWhiteboards(ctx, owner, identity, token)
			if e != nil {
				out["warnings"] = []string{"正文已读取，嵌入画板未完整读取：" + e.Error()}
			} else {
				out["whiteboards"] = boards
			}
		}
		return out, nil
	case "read_feishu_sheet", "read_feishu_sheet_metainfo", "list_feishu_sheet_tabs":
		if name == "read_feishu_sheet" {
			return s.readSheetTool(ctx, owner, identity, a)
		}
		token, e := s.spreadsheetToken(ctx, owner, identity, a)
		if e != nil {
			return nil, e
		}
		if name == "list_feishu_sheet_tabs" {
			tabs, e := s.sheetTabs(ctx, owner, identity, token)
			return map[string]any{"sheets": tabs}, e
		}
		if name == "read_feishu_sheet_metainfo" {
			return f("GET", "/open-apis/sheets/v2/spreadsheets/"+token+"/metainfo", nil, nil)
		}
		rangeA1 := valueOr(text(a, "range_a1"), text(a, "range"))
		if rangeA1 == "" {
			rangeA1 = "A1:AZ500"
		}
		sheet := text(a, "sheet_id")
		if !strings.Contains(rangeA1, "!") {
			if sheet == "" {
				tabs, e := s.sheetTabs(ctx, owner, identity, token)
				if e != nil {
					return nil, e
				}
				if len(tabs) == 0 {
					return nil, errors.New("没有可读工作表")
				}
				sheet = text(tabs[0], "sheet_id")
			}
			rangeA1 = sheet + "!" + rangeA1
		}
		if e := validSheetRange(rangeA1); e != nil {
			return nil, e
		}
		return f("GET", "/open-apis/sheets/v2/spreadsheets/"+token+"/values/"+rangeA1, nil, nil)
	case "search_feishu_documents":
		return f("POST", "/open-apis/search/v2/doc_wiki/search", nil, map[string]any{"query": text(a, "query"), "page_size": 50, "page_token": text(a, "page_token"), "doc_filter": map[string]any{}, "wiki_filter": map[string]any{}})
	case "list_feishu_drive_files":
		if folder := text(a, "folder_token"); folder != "" {
			q.Set("folder_token", folder)
		}
		q.Set("page_size", "50")
		return f("GET", "/open-apis/drive/v1/files", q, nil)
	case "create_feishu_docx":
		return f("POST", "/open-apis/docx/v1/documents", nil, map[string]any{"title": text(a, "title"), "folder_token": text(a, "folder_token")})
	case "update_feishu_docx_block":
		doc, e := segment(text(a, "document_id"))
		if e != nil {
			return nil, e
		}
		block, e := segment(text(a, "block_id"))
		if e != nil {
			return nil, e
		}
		body, e := parseJSON(a, "update_json")
		if e != nil {
			return nil, e
		}
		return f("PATCH", "/open-apis/docx/v1/documents/"+doc+"/blocks/"+block, nil, body)
	}
	return s.executeFeishu(ctx, c, name, a)
}
func parseJSON(a map[string]any, key string) (any, error) {
	var out any
	if err := json.Unmarshal([]byte(text(a, key)), &out); err != nil {
		return nil, fmt.Errorf("%s 不是有效 JSON", key)
	}
	return out, nil
}
func (s *Service) spreadsheetToken(ctx context.Context, owner, identity string, a map[string]any) (string, error) {
	if token := text(a, "spreadsheet_token"); token != "" {
		return segment(token)
	}
	raw := text(a, "url")
	if raw == "" {
		return "", errors.New("请提供 spreadsheet_token 或 url")
	}
	kind, token, _, err := s.resolveRef(ctx, owner, identity, raw)
	if err != nil {
		return "", err
	}
	if kind != "sheet" && kind != "sheets" {
		return "", errors.New("链接不是电子表格")
	}
	return token, nil
}
func validSheetRange(raw string) error {
	parts := strings.Split(raw, "!")
	if len(parts) != 2 || !tokenPattern.MatchString(parts[0]) || !rangePattern.MatchString(parts[1]) {
		return errors.New("range_a1 应为 sheet_id!A1:B10")
	}
	return nil
}

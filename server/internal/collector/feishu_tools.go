package collector

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ai-employee-platform/server/internal/mcpserver"
)

func (s *Service) primaryCalendar(ctx context.Context, owner, identity string) (string, error) {
	data, err := s.Feishu(ctx, owner, identity, "POST", "/open-apis/calendar/v4/calendars/primary", url.Values{"user_id_type": {"open_id"}}, map[string]any{})
	if err != nil {
		return "", err
	}
	for _, v := range array(data["calendars"]) {
		id := text(object(object(v)["calendar"]), "calendar_id")
		if id != "" {
			return id, nil
		}
	}
	return "", errors.New("当前身份没有主日历，请明确指定 calendar_id")
}

func (s *Service) executeFeishu(ctx context.Context, c *mcpserver.Caller, name string, a map[string]any) (any, error) {
	owner, identity := c.UserID, text(a, "identity")
	f := func(method, path string, q url.Values, body any) (map[string]any, error) {
		return s.Feishu(ctx, owner, identity, method, path, q, body)
	}
	userQ := url.Values{"user_id_type": {"open_id"}}
	// Validate identifiers before including them in a provider path.
	for _, k := range []string{"document_id", "block_id", "message_id", "spreadsheet_token", "sheet_id", "space_id", "parent_node_token", "node_token", "target_parent_token", "target_space_id", "app_token", "table_id", "record_id", "file_token"} {
		if v := text(a, k); v != "" {
			if _, err := segment(v); err != nil {
				return nil, fmt.Errorf("无效 %s", k)
			}
		}
	}
	switch name {
	case "send_feishu_message", "send_feishu_group_message", "send_feishu_card", "send_feishu_image", "send_feishu_file":
		id, kind := text(a, "open_id"), "open_id"
		if chat := text(a, "chat_id"); chat != "" {
			id, kind = chat, "chat_id"
		}
		if id == "" && text(a, "to_person") != "" {
			profile, err := s.Profile(ctx, owner)
			if err != nil {
				return nil, err
			}
			if matchesAny([]string{text(a, "to_person")}, append([]string{owner, profile.DisplayName, profile.FeishuName}, profile.Aliases...)) {
				id = profile.FeishuOpenID
			}
			if id == "" {
				return nil, errors.New("请先用 search_feishu_user 查找收件人的 open_id")
			}
		}
		if id == "" {
			return nil, errors.New("请指定收件人 open_id 或 chat_id")
		}
		msgType := "text"
		content := map[string]any{"text": text(a, "text")}
		if name == "send_feishu_card" {
			msgType = "interactive"
			elements := []any{map[string]any{"tag": "markdown", "content": text(a, "content")}}
			if target := text(a, "button_url"); target != "" {
				u, err := url.Parse(target)
				if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
					return nil, errors.New("按钮链接需为 HTTP(S)")
				}
				elements = append(elements, map[string]any{"tag": "action", "actions": []any{map[string]any{"tag": "button", "text": map[string]any{"tag": "plain_text", "content": valueOr(text(a, "button_text"), "打开")}, "url": target, "type": "primary"}}})
			}
			content = map[string]any{"config": map[string]bool{"wide_screen_mode": true}, "header": map[string]any{"title": map[string]string{"tag": "plain_text", "content": text(a, "title")}}, "elements": elements}
		}
		if name == "send_feishu_image" || name == "send_feishu_file" {
			uploaded, err := s.uploadMedia(ctx, owner, identity, name, a)
			if err != nil {
				return nil, err
			}
			if name == "send_feishu_image" {
				msgType = "image"
				content = map[string]any{"image_key": uploaded["image_key"]}
			} else {
				msgType = "file"
				content = map[string]any{"file_key": uploaded["file_key"]}
			}
		}
		payload, _ := json.Marshal(content)
		return f("POST", "/open-apis/im/v1/messages", url.Values{"receive_id_type": {kind}}, map[string]any{"receive_id": id, "msg_type": msgType, "content": string(payload), "uuid": randomID()[:32]})
	case "reply_feishu_message":
		content, _ := json.Marshal(map[string]string{"text": text(a, "text")})
		return f("POST", "/open-apis/im/v1/messages/"+text(a, "message_id")+"/reply", nil, map[string]any{"msg_type": "text", "content": string(content), "uuid": randomID()[:32]})
	case "search_feishu_user":
		if text(a, "emails") != "" || text(a, "mobiles") != "" {
			return f("POST", "/open-apis/contact/v3/users/batch_get_id", userQ, map[string]any{"emails": stringsOf(a["emails"]), "mobiles": stringsOf(a["mobiles"]), "include_resigned": false})
		}
		return f("GET", "/open-apis/search/v1/user", url.Values{"query": {text(a, "query")}, "page_size": {"20"}, "page_token": {text(a, "page_token")}}, nil)
	case "list_feishu_busy":
		ids := stringsOf(a["user_open_ids"])
		if len(ids) == 0 || len(ids) > 50 {
			return nil, errors.New("请提供 1–50 个用户 open_id")
		}
		results := []any{}
		for _, id := range ids {
			data, err := f("POST", "/open-apis/calendar/v4/freebusy/list", userQ, map[string]any{"time_min": text(a, "time_min"), "time_max": text(a, "time_max"), "user_id": id})
			if err != nil {
				return nil, err
			}
			results = append(results, map[string]any{"user_id": id, "freebusy_list": data["freebusy_list"]})
		}
		return map[string]any{"users": results}, nil
	case "create_feishu_event", "list_feishu_calendar_events":
		cal := text(a, "calendar_id")
		if cal == "" {
			var err error
			cal, err = s.primaryCalendar(ctx, owner, identity)
			if err != nil {
				return nil, err
			}
		}
		// Calendar IDs contain @group.calendar.feishu.cn; PathEscape prevents path injection.
		path := "/open-apis/calendar/v4/calendars/" + url.PathEscape(cal) + "/events"
		if name == "list_feishu_calendar_events" {
			q := url.Values{"page_size": {"50"}}
			for _, k := range []string{"start_time", "end_time", "page_token"} {
				if v := text(a, k); v != "" {
					q.Set(k, v)
				}
			}
			return f("GET", path, q, nil)
		}
		data, err := f("POST", path, userQ, map[string]any{"summary": text(a, "summary"), "description": text(a, "description"), "start_time": map[string]string{"timestamp": text(a, "start_timestamp")}, "end_time": map[string]string{"timestamp": text(a, "end_timestamp")}})
		if err != nil {
			return nil, err
		}
		event := object(data["event"])
		id := text(event, "event_id")
		attendees := []any{}
		for _, oid := range stringsOf(a["attendee_open_ids"]) {
			attendees = append(attendees, map[string]string{"type": "user", "user_id": oid})
		}
		if id != "" && len(attendees) > 0 {
			_, err = f("POST", path+"/"+url.PathEscape(id)+"/attendees", userQ, map[string]any{"attendees": attendees, "need_notification": true})
			if err != nil {
				return map[string]any{"status": "partial", "event": event, "message": "日程已创建，添加参会人失败；请勿重复创建"}, nil
			}
		}
		return data, nil
	case "list_feishu_tasks":
		userQ.Set("page_size", strconv.Itoa(min(50, max(1, integer(a, "page_size", 20)))))
		if next := text(a, "page_token"); next != "" {
			userQ.Set("page_token", next)
		}
		return f("GET", "/open-apis/task/v2/tasks", userQ, nil)
	case "create_feishu_task":
		body := map[string]any{"summary": text(a, "summary"), "description": text(a, "description")}
		if due := text(a, "due_timestamp"); due != "" {
			body["due"] = map[string]any{"timestamp": due, "is_all_day": false}
		}
		members := []any{}
		for _, id := range stringsOf(a["assignee_open_ids"]) {
			members = append(members, map[string]string{"id": id, "type": "user", "role": "assignee"})
		}
		if len(members) > 0 {
			body["members"] = members
		}
		return f("POST", "/open-apis/task/v2/tasks", userQ, body)
	case "append_feishu_docx":
		doc := text(a, "document_id")
		return f("POST", "/open-apis/docx/v1/documents/"+doc+"/blocks/"+doc+"/children", nil, map[string]any{
			"index": -1, "children": []any{map[string]any{"block_type": 2, "text": map[string]any{"elements": []any{map[string]any{"text_run": map[string]string{"content": text(a, "text")}}}}}}})
	case "write_feishu_sheet", "write_feishu_sheet_styles", "merge_feishu_sheet_cells", "unmerge_feishu_sheet_cells", "create_feishu_sheet_tab", "delete_feishu_sheet_tab", "rename_feishu_sheet_tab":
		token, err := s.spreadsheetToken(ctx, owner, identity, a)
		if err != nil {
			return nil, err
		}
		base := "/open-apis/sheets/v2/spreadsheets/" + token
		switch name {
		case "write_feishu_sheet":
			rangeA1 := text(a, "range_a1")
			if err = validSheetRange(rangeA1); err != nil {
				return nil, err
			}
			values, err := parseJSON(a, "values_json")
			if err != nil {
				return nil, err
			}
			return f("PUT", base+"/values", nil, map[string]any{"valueRange": map[string]any{"range": rangeA1, "values": values}})
		case "write_feishu_sheet_styles":
			styles, err := parseJSON(a, "styles_json")
			if err != nil {
				return nil, err
			}
			return f("PUT", base+"/styles_batch_update", nil, map[string]any{"data": styles})
		case "merge_feishu_sheet_cells", "unmerge_feishu_sheet_cells":
			rangeA1 := text(a, "range_a1")
			if err = validSheetRange(rangeA1); err != nil {
				return nil, err
			}
			suffix := "/merge_cells"
			body := map[string]any{"range": rangeA1, "mergeType": valueOr(text(a, "merge_type"), "MERGE_ALL")}
			if name == "unmerge_feishu_sheet_cells" {
				suffix = "/unmerge_cells"
				delete(body, "mergeType")
			}
			return f("POST", base+suffix, nil, body)
		case "create_feishu_sheet_tab":
			props := map[string]any{"title": text(a, "title")}
			if i := integer(a, "index", -1); i >= 0 {
				props["index"] = i
			}
			return f("POST", base+"/sheets_batch_update", nil, map[string]any{"requests": []any{map[string]any{"addSheet": map[string]any{"properties": props}}}})
		case "delete_feishu_sheet_tab":
			return f("POST", base+"/sheets_batch_update", nil, map[string]any{"requests": []any{map[string]any{"deleteSheet": map[string]string{"sheetId": text(a, "sheet_id")}}}})
		case "rename_feishu_sheet_tab":
			return f("POST", base+"/sheets_batch_update", nil, map[string]any{"requests": []any{map[string]any{"updateSheet": map[string]any{"properties": map[string]string{"sheetId": text(a, "sheet_id"), "title": text(a, "title")}}}}})
		}
	case "create_feishu_wiki_node", "move_feishu_wiki_node", "update_feishu_wiki_title":
		path := "/open-apis/wiki/v2/spaces/" + text(a, "space_id") + "/nodes"
		switch name {
		case "create_feishu_wiki_node":
			return f("POST", path, nil, map[string]any{"parent_node_token": text(a, "parent_node_token"), "title": text(a, "title"), "obj_type": valueOr(text(a, "obj_type"), "docx"), "node_type": "origin"})
		case "move_feishu_wiki_node":
			return f("POST", path+"/"+text(a, "node_token")+"/move", nil, map[string]any{"target_parent_token": text(a, "target_parent_token"), "target_space_id": valueOr(text(a, "target_space_id"), text(a, "space_id"))})
		case "update_feishu_wiki_title":
			return f("POST", path+"/"+text(a, "node_token")+"/update_title", nil, map[string]string{"title": text(a, "title")})
		}
	case "list_bitable_records", "create_bitable_record", "update_bitable_record":
		path := "/open-apis/bitable/v1/apps/" + text(a, "app_token") + "/tables/" + text(a, "table_id") + "/records"
		if name == "list_bitable_records" {
			userQ.Set("page_size", strconv.Itoa(min(500, max(1, integer(a, "page_size", 20)))))
			if next := text(a, "page_token"); next != "" {
				userQ.Set("page_token", next)
			}
			return f("GET", path, userQ, nil)
		}
		fields, err := parseJSON(a, "fields_json")
		if err != nil {
			return nil, err
		}
		method := "POST"
		if name == "update_bitable_record" {
			method = "PUT"
			path += "/" + text(a, "record_id")
		}
		return f(method, path, userQ, map[string]any{"fields": fields})
	case "add_feishu_doc_comment":
		return f("POST", "/open-apis/drive/v1/files/"+text(a, "file_token")+"/comments", url.Values{"file_type": {valueOr(text(a, "file_type"), "docx")}, "user_id_type": {"open_id"}},
			map[string]any{"reply_list": map[string]any{"replies": []any{map[string]any{"content": map[string]any{"elements": []any{map[string]any{"type": "text_run", "text_run": map[string]string{"text": text(a, "content")}}}}}}}})
	case "create_feishu_approval":
		if identity != "app" {
			return nil, errors.New("发起原生审批需要应用身份，请显式选择 identity=app；不会自动切换身份")
		}
		profile, err := s.Profile(ctx, owner)
		if err != nil {
			return nil, err
		}
		oid := valueOr(text(a, "user_open_id"), profile.FeishuOpenID)
		if oid == "" || oid != profile.FeishuOpenID {
			return nil, errors.New("只允许为已验证的本人飞书身份发起审批")
		}
		if _, err = parseJSON(a, "form_json"); err != nil {
			return nil, err
		}
		return f("POST", "/open-apis/approval/v4/instances", userQ, map[string]any{"approval_code": text(a, "approval_code"), "open_id": oid, "form": text(a, "form_json")})
	case "create_feishu_meeting":
		profile, err := s.Profile(ctx, owner)
		if err != nil {
			return nil, err
		}
		oid := valueOr(text(a, "owner_open_id"), profile.FeishuOpenID)
		start, errStart := strconv.ParseInt(text(a, "start_timestamp"), 10, 64)
		end, errEnd := strconv.ParseInt(text(a, "end_timestamp"), 10, 64)
		if errStart != nil || errEnd != nil || end <= start {
			return nil, errors.New("开始和结束时间须为有效秒级时间戳，且结束晚于开始")
		}
		data, err := f("POST", "/open-apis/vc/v1/reserves/apply", userQ, map[string]any{"owner_id": oid, "end_time": text(a, "end_timestamp"), "meeting_settings": map[string]any{"topic": text(a, "topic"), "auto_record": false}})
		if err != nil {
			return nil, err
		}
		return map[string]any{"reserve": data, "requested_start_time": start, "message": "会议预约已创建，有效期截至 end_timestamp；此接口不创建日历日程，开始时间请通过日程工具另行安排"}, nil
	}
	return nil, errors.New("工具尚未匹配到可执行操作")
}

func (s *Service) uploadMedia(ctx context.Context, owner, identity, name string, a map[string]any) (map[string]any, error) {
	payload, err := base64.StdEncoding.DecodeString(text(a, "file_base64"))
	if err != nil || len(payload) == 0 || len(payload) > 5<<20 {
		return nil, errors.New("文件须为有效 Base64，最大 5 MiB")
	}
	filename := text(a, "file_name")
	if filename == "" || strings.ContainsAny(filename, "\\/\r\n") || filename != filepath.Base(filename) {
		return nil, errors.New("file_name 只能是文件名")
	}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	path, field := "/open-apis/im/v1/files", "file"
	if name == "send_feishu_image" {
		path, field = "/open-apis/im/v1/images", "image"
		_ = writer.WriteField("image_type", "message")
	} else {
		_ = writer.WriteField("file_type", valueOr(text(a, "file_type"), "stream"))
		_ = writer.WriteField("file_name", filename)
	}
	part, err := writer.CreateFormFile(field, filename)
	if err != nil {
		return nil, err
	}
	if _, err = part.Write(payload); err != nil {
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	token, err := s.accessToken(ctx, owner, identity)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", s.FeishuURL+path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, errors.New("文件上传失败")
	}
	defer resp.Body.Close()
	var data map[string]any
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&data) != nil || resp.StatusCode != 200 || integer(data, "code", 0) != 0 {
		return nil, errors.New("飞书未接受文件，请检查身份和文件上传权限")
	}
	return object(data["data"]), nil
}

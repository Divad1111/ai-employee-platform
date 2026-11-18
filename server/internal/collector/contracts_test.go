package collector

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/mcpserver"
)

// Contracts are checked against the installed official Feishu Go SDK models:
// calendar/v4 freebusy, task/v2, search/v2 doc_wiki, drive/v1, docx/v1,
// wiki/v2, bitable/v1 and vc/v1. Every write goes through the public
// preview/confirmation flow before any mock upstream request is accepted.
func TestFeishuToolRequestContracts(t *testing.T) {
	cases := []struct {
		name, method, path string
		args               map[string]any
		want               map[string]any
	}{
		{"search_feishu_documents", "POST", "/search/v2/doc_wiki/search", map[string]any{"query": "周计划"}, map[string]any{"query": "周计划", "doc_filter": map[string]any{}}},
		{"list_feishu_drive_files", "GET", "/drive/v1/files", map[string]any{"folder_token": "folder"}, nil},
		{"create_feishu_docx", "POST", "/docx/v1/documents", map[string]any{"title": "计划"}, map[string]any{"title": "计划"}},
		{"update_feishu_docx_block", "PATCH", "/docx/v1/documents/doc/blocks/block", map[string]any{"document_id": "doc", "block_id": "block", "update_json": `{"update_text_elements":{"elements":[]}}`}, map[string]any{"update_text_elements": map[string]any{"elements": []any{}}}},
		{"append_feishu_docx", "POST", "/docx/v1/documents/doc/blocks/doc/children", map[string]any{"document_id": "doc", "text": "正文"}, map[string]any{"index": float64(-1)}},
		{"create_feishu_task", "POST", "/task/v2/tasks", map[string]any{"summary": "任务", "due_timestamp": "1800000000000", "assignee_open_ids": "ou_alice"}, map[string]any{"summary": "任务", "due": map[string]any{"timestamp": "1800000000000", "is_all_day": false}, "members": []any{map[string]any{"id": "ou_alice", "type": "user", "role": "assignee"}}}},
		{"list_feishu_tasks", "GET", "/task/v2/tasks", map[string]any{"page_size": 20, "page_token": "next"}, nil},
		{"list_feishu_busy", "POST", "/calendar/v4/freebusy/list", map[string]any{"time_min": "2026-11-18T09:00:00+08:00", "time_max": "2026-11-18T10:00:00+08:00", "user_open_ids": "ou_alice"}, map[string]any{"user_id": "ou_alice"}},
		{"create_feishu_event", "POST", "/calendar/v4/calendars/cal/events", map[string]any{"calendar_id": "cal", "summary": "讨论", "start_timestamp": "1800000000", "end_timestamp": "1800003600"}, map[string]any{"summary": "讨论", "start_time": map[string]any{"timestamp": "1800000000"}}},
		{"list_feishu_calendar_events", "GET", "/calendar/v4/calendars/cal/events", map[string]any{"calendar_id": "cal", "start_time": "1800000000", "page_token": "next"}, nil},
		{"send_feishu_message", "POST", "/im/v1/messages", map[string]any{"open_id": "ou_bob", "text": "test"}, map[string]any{"receive_id": "ou_bob", "msg_type": "text", "content": `{"text":"test"}`}},
		{"send_feishu_group_message", "POST", "/im/v1/messages", map[string]any{"chat_id": "oc_test", "text": "test"}, map[string]any{"receive_id": "oc_test"}},
		{"send_feishu_card", "POST", "/im/v1/messages", map[string]any{"open_id": "ou_bob", "title": "标题", "content": "正文"}, map[string]any{"msg_type": "interactive"}},
		{"reply_feishu_message", "POST", "/im/v1/messages/message/reply", map[string]any{"message_id": "message", "text": "回复"}, map[string]any{"msg_type": "text"}},
		{"search_feishu_user", "POST", "/contact/v3/users/batch_get_id", map[string]any{"emails": "test@example.com"}, map[string]any{"emails": []any{"test@example.com"}}},
		{"write_feishu_sheet", "PUT", "/sheets/v2/spreadsheets/sheet/values", map[string]any{"spreadsheet_token": "sheet", "range_a1": "tab!A1:B2", "values_json": `[["a","b"],["c","d"]]`}, map[string]any{"valueRange": map[string]any{"range": "tab!A1:B2", "values": []any{[]any{"a", "b"}, []any{"c", "d"}}}}},
		{"write_feishu_sheet_styles", "PUT", "/sheets/v2/spreadsheets/sheet/styles_batch_update", map[string]any{"spreadsheet_token": "sheet", "styles_json": `[]`}, map[string]any{"data": []any{}}},
		{"merge_feishu_sheet_cells", "POST", "/sheets/v2/spreadsheets/sheet/merge_cells", map[string]any{"spreadsheet_token": "sheet", "range_a1": "tab!A1:B2"}, map[string]any{"mergeType": "MERGE_ALL"}},
		{"unmerge_feishu_sheet_cells", "POST", "/sheets/v2/spreadsheets/sheet/unmerge_cells", map[string]any{"spreadsheet_token": "sheet", "range_a1": "tab!A1:B2"}, map[string]any{"range": "tab!A1:B2"}},
		{"create_feishu_sheet_tab", "POST", "/sheets/v2/spreadsheets/sheet/sheets_batch_update", map[string]any{"spreadsheet_token": "sheet", "title": "新表"}, map[string]any{"requests": []any{map[string]any{"addSheet": map[string]any{"properties": map[string]any{"title": "新表"}}}}}},
		{"delete_feishu_sheet_tab", "POST", "/sheets/v2/spreadsheets/sheet/sheets_batch_update", map[string]any{"spreadsheet_token": "sheet", "sheet_id": "tab"}, map[string]any{"requests": []any{map[string]any{"deleteSheet": map[string]any{"sheetId": "tab"}}}}},
		{"rename_feishu_sheet_tab", "POST", "/sheets/v2/spreadsheets/sheet/sheets_batch_update", map[string]any{"spreadsheet_token": "sheet", "sheet_id": "tab", "title": "新名"}, map[string]any{"requests": []any{map[string]any{"updateSheet": map[string]any{"properties": map[string]any{"sheetId": "tab", "title": "新名"}}}}}},
		{"create_feishu_wiki_node", "POST", "/wiki/v2/spaces/space/nodes", map[string]any{"space_id": "space", "parent_node_token": "parent", "title": "Wiki"}, map[string]any{"parent_node_token": "parent", "obj_type": "docx", "node_type": "origin"}},
		{"move_feishu_wiki_node", "POST", "/wiki/v2/spaces/space/nodes/node/move", map[string]any{"space_id": "space", "node_token": "node", "target_parent_token": "parent"}, map[string]any{"target_parent_token": "parent", "target_space_id": "space"}},
		{"update_feishu_wiki_title", "POST", "/wiki/v2/spaces/space/nodes/node/update_title", map[string]any{"space_id": "space", "node_token": "node", "title": "更新"}, map[string]any{"title": "更新"}},
		{"list_bitable_records", "GET", "/bitable/v1/apps/base/tables/table/records", map[string]any{"app_token": "base", "table_id": "table", "page_token": "next"}, nil},
		{"create_bitable_record", "POST", "/bitable/v1/apps/base/tables/table/records", map[string]any{"app_token": "base", "table_id": "table", "fields_json": `{"标题":"任务"}`}, map[string]any{"fields": map[string]any{"标题": "任务"}}},
		{"update_bitable_record", "PUT", "/bitable/v1/apps/base/tables/table/records/record", map[string]any{"app_token": "base", "table_id": "table", "record_id": "record", "fields_json": `{"标题":"更新"}`}, map[string]any{"fields": map[string]any{"标题": "更新"}}},
		{"add_feishu_doc_comment", "POST", "/drive/v1/files/doc/comments", map[string]any{"file_token": "doc", "content": "评论"}, nil},
		{"create_feishu_meeting", "POST", "/vc/v1/reserves/apply", map[string]any{"topic": "会议", "start_timestamp": "1800000000", "end_timestamp": "1800003600"}, map[string]any{"end_time": "1800003600", "meeting_settings": map[string]any{"topic": "会议", "auto_record": false}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := testService(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != tc.method || r.URL.Path != "/open-apis"+tc.path {
					t.Errorf("unexpected API %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer access-alice" {
					t.Error("user token missing")
				}
				if tc.args["page_token"] != nil && r.URL.Query().Get("page_token") != tc.args["page_token"] {
					t.Error("pagination lost")
				}
				if tc.method != "GET" {
					body := map[string]any{}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					for k, want := range tc.want {
						if digest(body[k]) != digest(want) {
							t.Errorf("%s: got %v want %v", k, body[k], want)
						}
					}
					if tc.name == "create_feishu_task" && body["task"] != nil {
						t.Error("task must not be nested")
					}
					if tc.name == "list_feishu_busy" && body["user_ids"] != nil {
						t.Error("freebusy only accepts one user_id")
					}
				}
				okJSON(w, map[string]any{"items": []any{}, "event": map[string]any{"event_id": "event"}})
			})
			seedIdentity(t, s, "alice", time.Now().Add(time.Hour))
			provider := &Provider{Service: s}
			caller := &mcpserver.Caller{Kind: "external_user", SubjectID: "alice", UserID: "alice", TokenID: "client", CanWrite: true, CanDelete: true}
			result, err := provider.Call(context.Background(), caller, tc.name, tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if pending, ok := result.(map[string]any); ok && pending["status"] == "confirmation_required" {
				if calls != 0 {
					t.Fatal("preview executed remote mutation")
				}
				if _, err := provider.Call(context.Background(), caller, tc.name, map[string]any{"confirm_token": pending["confirm_token"]}); err != nil {
					t.Fatal(err)
				}
			}
			if calls != 1 {
				t.Fatalf("expected one request, got %d", calls)
			}
		})
	}
}

func TestNewGrantClearsRefreshAndNestedRefreshCannotChangeUser(t *testing.T) {
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, map[string]any{"open_id": "ou-other", "name": "other"})
	})
	p := Profile{UserID: "alice", FeishuOpenID: "ou-alice", GrantedScopes: "old"}
	c := Credentials{AccessToken: "old", RefreshToken: "old-refresh"}
	ctx := context.Background()
	if err := s.acceptToken(ctx, &p, &c, map[string]any{"grant_type": "refresh_token", "data": map[string]any{"access_token": "new"}}); err == nil {
		t.Fatal("nested refresh changed user")
	}
	if err := s.acceptToken(ctx, &p, &c, map[string]any{"access_token": "new"}); err != nil {
		t.Fatal(err)
	}
	if c.RefreshToken != "" || p.GrantedScopes != "" || p.GrantID == "" {
		t.Fatal("new grant inherited old credentials")
	}
}

func TestMessagePaginationAndContentSheetRead(t *testing.T) {
	pages := 0
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			pages++
			if r.URL.Query().Get("start_time") == "" {
				t.Error("message watermark missing")
			}
			if pages == 2 && r.URL.Query().Get("page_token") != "next" {
				t.Error("cursor lost")
			}
			okJSON(w, map[string]any{"items": []any{map[string]any{"message_id": []string{"m1", "m2"}[pages-1], "body": map[string]any{"content": "test"}}}, "has_more": pages == 1, "page_token": "next"})
		case strings.HasSuffix(r.URL.Path, "/sheets/query"):
			okJSON(w, map[string]any{"sheets": []any{map[string]any{"sheet_id": "tab", "title": "普通内容"}}})
		default:
			okJSON(w, map[string]any{"valueRange": map[string]any{"values": []any{[]any{"名称", "数值"}, []any{"甲", "1"}, []any{"乙", "2"}}}})
		}
	})
	seedIdentity(t, s, "alice", time.Now().Add(time.Hour))
	p, _ := s.Profile(context.Background(), "alice")
	items, err := s.fetchMessages(context.Background(), Target{ID: "im", OwnerID: "alice", Config: map[string]any{"chat_id": "chat", "only_mentioned": false}}, p, time.Now().Add(-time.Hour))
	if err != nil || len(items) != 2 || pages != 2 {
		t.Fatalf("pagination: %v %v", items, err)
	}
	result, err := s.readSheetTool(context.Background(), "alice", "user", map[string]any{"url": "https://test.feishu.cn/sheets/doc"})
	if err != nil || result.(map[string]any)["count"] != 2 {
		t.Fatalf("ordinary sheet: %v %v", result, err)
	}
}

func TestQueuedScanCrossRunBatchesAndNormalizedChanges(t *testing.T) {
	status := "进行中"
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sheets/query") {
			okJSON(w, map[string]any{"sheets": []any{map[string]any{"sheet_id": "tab", "title": "任务"}}})
		} else {
			okJSON(w, map[string]any{"valueRange": map[string]any{"values": []any{[]any{"单号", "标题", "开发人", "状态"}, []any{"T1", "任务", "alice", status}}}})
		}
	})
	ctx := context.Background()
	seedIdentity(t, s, "alice", time.Now().Add(time.Hour))
	p, _ := s.Profile(ctx, "alice")
	p.NotifyEnabled = true
	_ = s.saveProfile(ctx, p, nil)
	target, err := s.SaveTarget(ctx, "alice", Target{Name: "sheet", Source: "sheet", URL: "https://test.feishu.cn/sheets/doc", Config: map[string]any{"sheet_ids": []string{"tab"}}, NotifyBaseline: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveRule(ctx, "alice", Rule{Name: "状态变更", Enabled: true, Action: "batched", AggregateMinutes: 1440, Conditions: []Condition{{Field: "status", Operator: "changed"}}})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := s.RequestScan(ctx, "alice", target.ID)
	if err != nil || queued.Status != "queued" {
		t.Fatalf("enqueue: %v", err)
	}
	again, _ := s.RequestScan(ctx, "alice", target.ID)
	if again.ID != queued.ID {
		t.Fatal("duplicate queue request")
	}
	first, err := s.Scan(ctx, "alice", target.ID, false, nil)
	if err != nil || first.ID != queued.ID {
		t.Fatalf("queued scan: %v", err)
	}
	status = "完成"
	second, err := s.Scan(ctx, "alice", target.ID, false, nil)
	if err != nil || second.Changes != 1 || !contains(second.Events[0].ChangedFields, "status") {
		t.Fatalf("normalized status missing: %v %v", second, err)
	}
	deliveries, _ := list[Delivery](ctx, s.Store, "alice", "delivery")
	if len(deliveries) != 1 || len(deliveries[0].Events) != 2 {
		t.Fatalf("cross-run batch missing: %v", deliveries)
	}
	if _, err := load[Run](ctx, s.Store, "alice", "request", target.ID); err != ErrNotFound {
		t.Fatal("finished request remains queued")
	}
}

func TestTimezoneAndUnsafeRuleImport(t *testing.T) {
	if got := dateValueAt(float64(1800000000000), "Asia/Shanghai"); got != time.UnixMilli(1800000000000).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02") {
		t.Fatal(got)
	}
	s := testService(t, nil)
	if _, err := s.Import(context.Background(), "alice", "rules", "rules:\n- source: jira\n  when:\n    projects: [PRIVATE]\n  notify: instant\n", true); err == nil {
		t.Fatal("lossy rule imported")
	}
	rows, _ := s.List(context.Background(), "alice", "rule")
	if len(rows) != 0 {
		t.Fatal("partial import")
	}
}

func TestMeegoPluginTokenObtainedOnceAndStoredPrivately(t *testing.T) {
	calls := 0
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/open_api/authen/plugin_token" {
			t.Error(r.URL.Path)
		}
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["plugin_id"] != "plugin" || body["plugin_secret"] != "synthetic-secret" {
			t.Error("plugin credentials missing")
		}
		okJSON(w, map[string]any{"token": "synthetic-plugin-token", "expire_time": time.Now().Add(time.Hour).Unix()})
	})
	ctx := context.Background()
	if err := s.saveProfile(ctx, Profile{UserID: "alice", MeegoPluginID: "plugin", MeegoURL: s.FeishuURL}, &Credentials{MeegoSecret: "synthetic-secret"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if token, err := s.meegoToken(ctx, "alice"); err != nil || token != "synthetic-plugin-token" {
			t.Fatalf("plugin token: %v", err)
		}
	}
	if calls != 1 {
		t.Fatal("plugin token cache not reused")
	}
	status, err := s.ProfileStatus(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(status)
	if strings.Contains(string(encoded), "synthetic-") {
		t.Fatal("plugin secrets in public status")
	}
}

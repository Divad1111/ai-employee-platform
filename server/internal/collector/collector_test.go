package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/mcpserver"
	"github.com/ai-employee-platform/server/internal/secret"
)

func testService(t *testing.T, h http.HandlerFunc) *Service {
	t.Helper()
	vault, err := secret.NewMemoryVault()
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(NewMemoryStore(), secret.NewManager(vault, secret.NewMemoryBindings(), nil))
	s.UserActive = func(_ context.Context, owner string) bool { return owner == "alice" || owner == "bob" }
	if h != nil {
		server := httptest.NewServer(h)
		t.Cleanup(server.Close)
		s.FeishuURL, s.OAuthURL, s.AuthorizeURL = server.URL, server.URL+"/oauth/token", server.URL+"/authorize"
		s.HTTP = server.Client()
		s.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return s
}
func seedIdentity(t *testing.T, s *Service, owner string, expires time.Time) {
	t.Helper()
	if err := s.saveProfile(context.Background(), Profile{UserID: owner, DisplayName: owner, FeishuAppID: "app-" + owner, FeishuOpenID: "ou-" + owner, Timezone: "Asia/Shanghai", Scopes: "offline_access docx:document:readonly", ExpiresAt: &expires},
		&Credentials{AppSecret: "secret-" + owner, AccessToken: "access-" + owner, RefreshToken: "refresh-" + owner}); err != nil {
		t.Fatal(err)
	}
}
func okJSON(w http.ResponseWriter, data any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
}

func TestUserIdentityIsolationRefreshAndNoFallback(t *testing.T) {
	ctx := context.Background()
	var refreshes atomic.Int32
	var apps atomic.Int32
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			refreshes.Add(1)
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["refresh_token"] != "refresh-alice" || body["client_id"] != "app-alice" {
				t.Errorf("wrong credential owner")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-alice", "refresh_token": "rotated-alice", "expires_in": 7200})
		case "/open-apis/authen/v1/user_info":
			if r.Header.Get("Authorization") != "Bearer new-alice" {
				t.Error("wrong user token")
			}
			okJSON(w, map[string]string{"open_id": "ou-alice", "name": "Alice"})
		case "/open-apis/docx/v1/documents/test/raw_content":
			okJSON(w, map[string]string{"content": r.Header.Get("Authorization")})
		default:
			apps.Add(1)
			http.Error(w, "unexpected", 500)
		}
	})
	seedIdentity(t, s, "alice", time.Now().Add(-time.Hour))
	seedIdentity(t, s, "bob", time.Now().Add(time.Hour))
	data, err := s.Feishu(ctx, "alice", "user", "GET", "/open-apis/docx/v1/documents/test/raw_content", nil, nil)
	if err != nil || data["content"] != "Bearer new-alice" {
		t.Fatalf("refresh: %v", err)
	}
	data, err = s.Feishu(ctx, "bob", "user", "GET", "/open-apis/docx/v1/documents/test/raw_content", nil, nil)
	if err != nil || data["content"] != "Bearer access-bob" {
		t.Fatal("bob did not use own credential")
	}
	status, err := s.ProfileStatus(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(status)
	if strings.Contains(string(encoded), "rotated-alice") || strings.Contains(string(encoded), "secret-alice") || strings.Contains(string(encoded), "secret_ref") {
		t.Fatal("status leaked secret")
	}
	profile, _ := s.Profile(ctx, "alice")
	credentials, _ := s.credentials(ctx, profile)
	if credentials.RefreshToken != "rotated-alice" || refreshes.Load() != 1 {
		t.Fatal("rotated refresh token not persisted")
	}
	profile.SecretRef = ""
	profile.FeishuOpenID = ""
	_ = s.saveProfile(ctx, profile, &Credentials{AppSecret: "secret-alice"})
	if _, err = s.Feishu(ctx, "alice", "user", "GET", "/open-apis/docx/v1/documents/test/raw_content", nil, nil); err == nil {
		t.Fatal("missing personal authorization accepted")
	}
	if apps.Load() != 0 {
		t.Fatal("silently fell back to application token")
	}
}

func TestOAuthStateBoundToBrowserAndSingleUse(t *testing.T) {
	ctx := context.Background()
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["redirect_uri"] != "https://platform.example/api/collector/oauth/callback" || in["code_verifier"] == "" {
				t.Error("OAuth binding missing")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "oauth-alice", "refresh_token": "refresh", "expires_in": 7200})
		} else if r.URL.Path == "/open-apis/authen/v1/user_info" {
			okJSON(w, map[string]string{"open_id": "ou-alice", "name": "Alice"})
		} else {
			http.Error(w, "unexpected", 500)
		}
	})
	seedIdentity(t, s, "alice", time.Now().Add(time.Hour))
	link, err := s.BeginOAuth(ctx, "alice", "https://platform.example/api/collector/oauth/callback", "browser-a")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(link)
	state := u.Query().Get("state")
	if u.Query().Get("code_challenge") == "" || state == "" {
		t.Fatal("missing PKCE or state")
	}
	if _, err = s.CompleteOAuth(ctx, state, "code", "browser-b"); err == nil {
		t.Fatal("different browser consumed state")
	}
	if owner, err := s.CompleteOAuth(ctx, state, "code", "browser-a"); err != nil || owner != "alice" {
		t.Fatalf("OAuth failed: %v", err)
	}
	if _, err = s.CompleteOAuth(ctx, state, "code", "browser-a"); err == nil {
		t.Fatal("OAuth state replayed")
	}
}

func TestWriteConfirmationBindsPrincipalArgumentsIdentityAndIsAtomic(t *testing.T) {
	ctx := context.Background()
	var writes atomic.Int32
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		writes.Add(1)
		okJSON(w, map[string]string{"message_id": "msg"})
	})
	seedIdentity(t, s, "alice", time.Now().Add(time.Hour))
	seedIdentity(t, s, "bob", time.Now().Add(time.Hour))
	p := &Provider{Service: s}
	caller := &mcpserver.Caller{Kind: "external_user", SubjectID: "alice", UserID: "alice", TokenID: "token-a", CanWrite: true}
	args := map[string]any{"open_id": "ou-destination", "text": "hello"}
	out, err := p.Call(ctx, caller, "send_feishu_message", args)
	if err != nil {
		t.Fatal(err)
	}
	token := text(object(out), "confirm_token")
	if token == "" || writes.Load() != 0 {
		t.Fatal("preview performed a write")
	}
	other := *caller
	other.TokenID = "other-client"
	if _, err = p.Call(ctx, &other, "send_feishu_message", map[string]any{"confirm_token": token}); err == nil {
		t.Fatal("other client consumed confirmation")
	}
	other = *caller
	other.SubjectID, other.UserID = "bob", "bob"
	if _, err = p.Call(ctx, &other, "send_feishu_message", map[string]any{"confirm_token": token}); err == nil {
		t.Fatal("cross-user confirmation accepted")
	}
	if _, err = p.Call(ctx, caller, "send_feishu_message", map[string]any{"confirm_token": token, "open_id": "other", "text": "hello"}); err == nil {
		t.Fatal("modified payload accepted")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = p.Call(ctx, caller, "send_feishu_message", map[string]any{"confirm_token": token})
		}()
	}
	wg.Wait()
	if writes.Load() != 1 {
		t.Fatalf("expected exactly one execution, got %d", writes.Load())
	}
	readonly := *caller
	readonly.CanWrite = false
	if _, err = p.Call(ctx, &readonly, "send_feishu_message", args); err == nil {
		t.Fatal("read credential prepared a write")
	}
	if _, err = p.Call(ctx, caller, "delete_feishu_sheet_tab", map[string]any{"sheet_id": "tab", "spreadsheet_token": "doc"}); err == nil {
		t.Fatal("WRITE included deletion")
	}
}

func TestScanPreviewBaselineStableKeysFailureAndOwnerIsolation(t *testing.T) {
	ctx := context.Background()
	var phase atomic.Int32
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		if phase.Load() == 3 {
			http.Error(w, `{"code":99991672}`, 403)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/sheets/query"):
			okJSON(w, map[string]any{"sheets": []any{map[string]any{"sheet_id": "tab", "title": "Main"}}})
		case strings.Contains(r.URL.Path, "/values/"):
			rows := []any{[]any{"单号", "标题", "开发人", "状态", "截止日期"}, []any{"A-1", "First", "alice", "进行中", "2026-11-19"}, []any{"A-2", "Second", "alice", "进行中", "2026-11-20"}}
			if phase.Load() == 1 {
				rows[1], rows[2] = rows[2], rows[1]
			}
			if phase.Load() == 2 {
				rows[1] = []any{"A-1", "First", "alice", "已完成", "2026-11-19"}
			}
			okJSON(w, map[string]any{"valueRange": map[string]any{"values": rows}})
		default:
			http.Error(w, "unexpected", 500)
		}
	})
	seedIdentity(t, s, "alice", time.Now().Add(time.Hour))
	target, err := s.SaveTarget(ctx, "alice", Target{Name: "Weekly", Source: "sheet", URL: "https://example.feishu.cn/sheets/token?sheet=tab", Config: map[string]any{"person_columns": []string{"开发人"}}, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.Scan(ctx, "alice", target.ID, true, nil)
	if err != nil || run.Count != 2 {
		t.Fatalf("preview: %v", err)
	}
	if _, err := s.Store.Get(ctx, "alice", "snapshot", target.ID); err != ErrNotFound {
		t.Fatal("preview wrote a snapshot")
	}
	if _, err := s.Store.Get(ctx, "alice", "run", run.ID); err != ErrNotFound {
		t.Fatal("preview mutated production run")
	}
	_, _ = s.SaveRule(ctx, "alice", Rule{Name: "Status", Enabled: true, Match: "all", Action: "instant", Conditions: []Condition{{"状态", "changed", ""}}})
	run, err = s.Scan(ctx, "alice", target.ID, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range run.Events {
		if ev.Action != "record" {
			t.Fatal("baseline notified")
		}
	}
	phase.Store(1)
	run, err = s.Scan(ctx, "alice", target.ID, false, nil)
	if err != nil || run.Changes != 0 {
		t.Fatalf("row sorting caused changes: %d %v", run.Changes, err)
	}
	phase.Store(2)
	run, err = s.Scan(ctx, "alice", target.ID, false, nil)
	if err != nil || run.Changes != 1 || run.Events[0].Action != "instant" {
		t.Fatalf("missed change: %+v %v", run, err)
	}
	before, _ := s.Store.Get(ctx, "alice", "snapshot", target.ID)
	phase.Store(3)
	if _, err = s.Scan(ctx, "alice", target.ID, false, nil); err == nil {
		t.Fatal("upstream failure accepted")
	}
	after, _ := s.Store.Get(ctx, "alice", "snapshot", target.ID)
	if string(before) != string(after) {
		t.Fatal("failed scan advanced snapshot")
	}
	if _, err = s.Scan(ctx, "bob", target.ID, false, nil); err != ErrNotFound {
		t.Fatal("cross-user scan allowed")
	}
	items, _ := s.Items(ctx, "bob")
	if len(items) != 0 {
		t.Fatal("cross-user aggregation leak")
	}
}

func TestRulesAndCatalog(t *testing.T) {
	s := testService(t, nil)
	p := &Provider{Service: s}
	names := map[string]bool{}
	for _, tool := range p.Tools() {
		name := text(tool, "name")
		if names[name] {
			t.Fatal("duplicate tool")
		}
		names[name] = true
	}
	if len(names) != 53 || !names["search_feishu_wiki"] || !names["search_feishu_documents"] || !names["write_feishu_sheet"] {
		t.Fatalf("incomplete catalog: %d", len(names))
	}
	event := Event{Item: Item{Source: "sheet", Status: "进行中", Assignees: []string{"开发甲"}, Fields: map[string]any{"状态": "进行中"}}, ChangedFields: []string{"状态"}, Kind: "updated"}
	profile := Profile{DisplayName: "张三", Aliases: []string{"开发甲"}}
	rules := []Rule{{ID: "1", Name: "Only me", Enabled: true, Action: "instant", Match: "all", Conditions: []Condition{{"assignee", "is_me", ""}, {"状态", "changed", ""}}}}
	action, _, _, _ := Evaluate(rules, event, profile, time.Now())
	if action != "instant" {
		t.Fatal("alias match failed")
	}
	profile.Aliases = nil
	action, _, _, _ = Evaluate(rules, event, profile, time.Now())
	if action != "record" {
		t.Fatal("unmatched rule notified")
	}
}

func TestNoSecretEchoOrRedirect(t *testing.T) {
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://example.invalid")
		w.WriteHeader(302)
		fmt.Fprint(w, `{"error":"secret-that-must-not-escape"}`)
	})
	_, err := s.request(context.Background(), "GET", s.FeishuURL, "synthetic-token", nil, nil)
	if err == nil || strings.Contains(err.Error(), "secret-that-must-not-escape") || strings.Contains(err.Error(), "synthetic-token") {
		t.Fatal("untrusted upstream response leaked")
	}
}

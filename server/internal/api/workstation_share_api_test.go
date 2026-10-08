package api_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"net/http"
	"testing"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/enrollment"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/workstation"
	"github.com/ai-employee-platform/server/internal/wsmember"
)

func TestWorkstationPublicShareCreatorOnly(t *testing.T) {
	auditor := audit.NewMemory()
	users := auth.NewMemoryUserStore()
	if err := users.SeedAdmin("admin", "admin12345", "Admin"); err != nil {
		t.Fatal(err)
	}
	ca, err := certca.NewDevAuthority()
	if err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), auditor)
	h := api.NewRouter(api.Deps{
		Auth:         authSvc,
		Audit:        auditor,
		Employees:    employee.NewService(employee.NewMemoryStore(), auditor, nil),
		Enrollment:   enrollment.NewService(enrollment.NewMemoryStore(), ca, auditor),
		CA:           ca,
		Workstations: workstation.NewService(ca, reliability.NewPresence(5, 15), workstation.NewMemoryMeta()),
		WSMembers:    wsmember.NewMemoryStore(),
		WSShare:      workstation.NewMemoryShare(),
	})

	login := func(user, pass string) string {
		t.Helper()
		code, resp := doJSON(t, h, http.MethodPost, "/api/auth/login", "", map[string]string{"username": user, "password": pass})
		if code != http.StatusOK {
			t.Fatalf("login %s: %d %v", user, code, resp)
		}
		return resp["token"].(string)
	}
	createUser := func(tok, name, pass string, roles []string) string {
		t.Helper()
		code, created := doJSON(t, h, http.MethodPost, "/api/users", tok, map[string]any{
			"username": name, "password": pass, "roles": roles,
		})
		if code != http.StatusCreated {
			t.Fatalf("create %s: %d %v", name, code, created)
		}
		return created["id"].(string)
	}
	adminTok := login("admin", "admin12345")
	ownerID := createUser(adminTok, "owner", "owner12345", []string{"USER"})
	opID := createUser(adminTok, "operator", "op1234567", []string{"OPERATOR"})
	viewerID := createUser(adminTok, "viewer", "viewer12345", []string{"VIEWER"})
	peerID := createUser(adminTok, "peer", "peer123456", []string{"USER"})
	hash, err := auth.HashPassword("super12345")
	if err != nil {
		t.Fatal(err)
	}
	super := &auth.User{Username: "root", PasswordHash: hash, DisplayName: "Root", Status: auth.StatusActive, Roles: []string{"SUPER_ADMIN"}}
	if err := users.Create(t.Context(), super); err != nil {
		t.Fatal(err)
	}
	ownerTok := login("owner", "owner12345")
	opTok := login("operator", "op1234567")
	viewerTok := login("viewer", "viewer12345")
	peerTok := login("peer", "peer123456")
	superTok := login("root", "super12345")

	if code, body := doJSON(t, h, http.MethodPost, "/api/enrollment/tokens", ownerTok, map[string]any{
		"label": "pub", "public": true, "grant_roles": []string{},
	}); code != http.StatusBadRequest {
		t.Fatalf("公用但未选角色应 400: %d %v", code, body)
	}

	code, priv := doJSON(t, h, http.MethodPost, "/api/enrollment/tokens", ownerTok, map[string]any{
		"label": "priv-node", "public": false, "grant_roles": []string{"OPERATOR"},
	})
	if code != http.StatusCreated {
		t.Fatalf("私有令牌: %d %v", code, priv)
	}
	enrollWS(t, h, priv["token"].(string), "WS-PRIV")
	if ids := listWSIDs(t, h, opTok); containsStr(ids, "WS-PRIV") {
		t.Fatal("未勾选公用时，令牌上的角色不应生效")
	}
	_, privShare := doJSON(t, h, http.MethodGet, "/api/workstations/WS-PRIV/sharing", ownerTok, nil)
	if privShare["is_public"] == true || len(asStrings(privShare["grant_roles"])) != 0 {
		t.Fatalf("私有工作站不应保留角色授权: %+v", privShare)
	}
	if privShare["can_manage"] != true || privShare["created_by_user_id"] != ownerID {
		t.Fatalf("创建者应能管理: %+v", privShare)
	}

	code, pub := doJSON(t, h, http.MethodPost, "/api/enrollment/tokens", ownerTok, map[string]any{
		"label": "pub-node", "public": true, "grant_roles": []string{" operator ", "OPERATOR"},
	})
	if code != http.StatusCreated {
		t.Fatalf("公用令牌: %d %v", code, pub)
	}
	enrollWS(t, h, pub["token"].(string), "WS-PUB")

	if ids := listWSIDs(t, h, opTok); !containsStr(ids, "WS-PUB") || containsStr(ids, "WS-PRIV") {
		t.Fatalf("操作员应只看到授权的公用站: %v", ids)
	}
	if ids := listWSIDs(t, h, viewerTok); containsStr(ids, "WS-PUB") {
		t.Fatal("未授权角色不应看到公用站")
	}
	if ids := listWSIDs(t, h, ownerTok); !containsStr(ids, "WS-PUB") || !containsStr(ids, "WS-PRIV") {
		t.Fatalf("创建者应看到自己的工作站: %v", ids)
	}

	_, opShare := doJSON(t, h, http.MethodGet, "/api/workstations/WS-PUB/sharing", opTok, nil)
	if opShare["can_manage"] != false || opShare["is_public"] != true {
		t.Fatalf("非创建者只能查看: %+v", opShare)
	}
	if _, ok := opShare["users"]; ok && opShare["users"] != nil {
		t.Fatalf("非创建者不应拿到用户列表: %+v", opShare["users"])
	}
	if roles := asStrings(opShare["grant_roles"]); len(roles) != 1 {
		t.Fatalf("重复角色应合并: %+v", opShare["grant_roles"])
	}

	if code, body := doJSON(t, h, http.MethodPut, "/api/workstations/WS-PUB/sharing", opTok, map[string]any{
		"public": true, "grant_roles": []string{"VIEWER"},
	}); code != http.StatusForbidden || body["error"] != "只有工作站创建者可以调整授权" {
		t.Fatalf("操作员不能改授权: %d %v", code, body)
	}
	if code, body := doJSON(t, h, http.MethodPut, "/api/workstations/WS-PUB/sharing", superTok, map[string]any{
		"public": true, "grant_roles": []string{"VIEWER"},
	}); code != http.StatusForbidden || body["error"] != "只有工作站创建者可以调整授权" {
		t.Fatalf("超级管理员也不是创建者，不能改授权: %d %v", code, body)
	}
	if code, body := doJSON(t, h, http.MethodPut, "/api/workstations/WS-PUB/sharing", ownerTok, map[string]any{
		"public": true, "grant_roles": []string{" "},
	}); code != http.StatusBadRequest {
		t.Fatalf("公用必须选角色: %d %v", code, body)
	}

	if code, body := doJSON(t, h, http.MethodPost, "/api/employees", peerTok, map[string]any{
		"name": "nope", "workstation_id": "WS-PUB",
	}); code != http.StatusForbidden || body["error"] != "WORKSTATION_ACCESS_DENIED" {
		t.Fatalf("未授权用户不能在该站建员工: %d %v", code, body)
	}
	if code, body := doJSON(t, h, http.MethodPost, "/api/employees", opTok, map[string]any{
		"name": "ok", "workstation_id": "WS-PUB",
	}); code != http.StatusCreated {
		t.Fatalf("角色命中的用户应能使用工作站: %d %v", code, body)
	}

	code, saved := doJSON(t, h, http.MethodPut, "/api/workstations/WS-PUB/sharing", ownerTok, map[string]any{
		"public": true, "grant_roles": []string{"USER"}, "member_user_ids": []string{viewerID},
	})
	if code != http.StatusOK {
		t.Fatalf("创建者保存授权: %d %v", code, saved)
	}
	if !containsStr(asStrings(saved["grant_roles"]), "USER") {
		t.Fatalf("应改为授权 USER: %+v", saved["grant_roles"])
	}
	if !memberHas(saved["members"], viewerID, "MEMBER") || !memberHas(saved["members"], ownerID, "OWNER") {
		t.Fatalf("成员应含创建者与被勾选用户: %+v", saved["members"])
	}
	if ids := listWSIDs(t, h, viewerTok); !containsStr(ids, "WS-PUB") {
		t.Fatal("单独授权的用户应能看到工作站")
	}
	if ids := listWSIDs(t, h, opTok); containsStr(ids, "WS-PUB") {
		t.Fatal("角色改掉后，原角色用户不应再看到")
	}
	if code, body := doJSON(t, h, http.MethodPost, "/api/employees", peerTok, map[string]any{
		"name": "peer-emp", "workstation_id": "WS-PUB",
	}); code != http.StatusCreated {
		t.Fatalf("改成 USER 后，同角色用户应能使用: %d %v", code, body)
	}
	_, ownerWhilePublic := doJSON(t, h, http.MethodGet, "/api/users/"+ownerID, adminTok, nil)
	if wsGrantHas(ownerWhilePublic["workstations"], "WS-PUB", "角色授权") {
		t.Fatal("创建者已是成员，不应再重复记成角色授权")
	}
	_, opDetail := doJSON(t, h, http.MethodGet, "/api/users/"+opID, adminTok, nil)
	if wsGrantHas(opDetail["workstations"], "WS-PUB", "角色授权") {
		t.Fatalf("角色不再匹配时不应显示角色授权: %+v", opDetail["workstations"])
	}

	code, closed := doJSON(t, h, http.MethodPut, "/api/workstations/WS-PUB/sharing", ownerTok, map[string]any{
		"public": false, "grant_roles": []string{"OPERATOR"}, "member_user_ids": []string{viewerID},
	})
	if code != http.StatusOK {
		t.Fatalf("取消公用: %d %v", code, closed)
	}
	if closed["is_public"] == true || len(asStrings(closed["grant_roles"])) != 0 {
		t.Fatalf("取消公用应清掉角色: %+v", closed)
	}
	if !memberHas(closed["members"], viewerID, "MEMBER") {
		t.Fatalf("取消公用后单独授权的用户应保留: %+v", closed["members"])
	}
	if ids := listWSIDs(t, h, peerTok); containsStr(ids, "WS-PUB") {
		t.Fatal("取消公用后，仅凭角色不应再看到工作站")
	}

	if code, body := doJSON(t, h, http.MethodDelete, "/api/workstations/WS-PUB/members/"+ownerID, ownerTok, nil); code != http.StatusBadRequest {
		t.Fatalf("不能取消创建者: %d %v", code, body)
	}
	if code, body := doJSON(t, h, http.MethodDelete, "/api/workstations/WS-PUB/members/"+viewerID, peerTok, nil); code != http.StatusForbidden || body["error"] != "只有工作站创建者可以调整授权" {
		t.Fatalf("非创建者不能删成员: %d %v", code, body)
	}
	if code, body := doJSON(t, h, http.MethodPost, "/api/workstations/WS-PUB/members", peerTok, map[string]any{"user_id": peerID}); code != http.StatusForbidden || body["error"] != "只有工作站创建者可以调整授权" {
		t.Fatalf("非创建者不能加成员: %d %v", code, body)
	}
}

func TestUserOwnCannotWriteOthersWorkstation(t *testing.T) {
	auditor := audit.NewMemory()
	users := auth.NewMemoryUserStore()
	if err := users.SeedAdmin("admin", "admin12345", "Admin"); err != nil {
		t.Fatal(err)
	}
	ca, err := certca.NewDevAuthority()
	if err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), auditor)
	share := workstation.NewMemoryShare()
	nodes := workstation.NewService(ca, reliability.NewPresence(5, 15), workstation.NewMemoryMeta())
	h := api.NewRouter(api.Deps{
		Auth:         authSvc,
		Audit:        auditor,
		Workstations: nodes,
		WSShare:      share,
	})
	login := func(user, pass string) string {
		t.Helper()
		code, resp := doJSON(t, h, http.MethodPost, "/api/auth/login", "", map[string]string{"username": user, "password": pass})
		if code != http.StatusOK {
			t.Fatalf("login %s: %d %v", user, code, resp)
		}
		return resp["token"].(string)
	}
	adminTok := login("admin", "admin12345")
	code, created := doJSON(t, h, http.MethodPost, "/api/users", adminTok, map[string]any{
		"username": "zhangwei", "password": "zhangwei12", "roles": []string{"USER"},
	})
	if code != http.StatusCreated {
		t.Fatalf("create user: %d %v", code, created)
	}
	zhangweiID := created["id"].(string)
	admin, err := users.FindByUsername(t.Context(), "admin")
	if err != nil || admin == nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	nodes.EnsureRegistered(ctx, "WS-other", "别人的站")
	nodes.EnsureRegistered(ctx, "WS-mine", "自己的站")
	if err := share.Save(ctx, workstation.Share{WorkstationID: "WS-other", IsPublic: true, CreatedBy: admin.ID, Roles: []string{"USER"}}); err != nil {
		t.Fatal(err)
	}
	if err := share.Save(ctx, workstation.Share{WorkstationID: "WS-mine", IsPublic: true, CreatedBy: zhangweiID, Roles: []string{"USER"}}); err != nil {
		t.Fatal(err)
	}
	userTok := login("zhangwei", "zhangwei12")
	code, listed := doJSON(t, h, http.MethodGet, "/api/workstations", userTok, nil)
	if code != http.StatusOK {
		t.Fatalf("list: %d %v", code, listed)
	}
	canWrite := map[string]bool{}
	for _, item := range listed["items"].([]any) {
		row := item.(map[string]any)
		canWrite[row["id"].(string)] = row["can_write"] == true
	}
	if canWrite["WS-other"] || !canWrite["WS-mine"] {
		t.Fatalf("仅本人应只能写自己创建的工作站: %+v", canWrite)
	}
	if code, body := doJSON(t, h, http.MethodPatch, "/api/workstations/WS-other", userTok, map[string]string{"name": "改名"}); code != http.StatusForbidden {
		t.Fatalf("仅本人不能改别人的工作站: %d %v", code, body)
	}
	code, stepped := doJSON(t, h, http.MethodPost, "/api/auth/step-up", userTok, map[string]string{"password": "zhangwei12"})
	if code != http.StatusOK {
		t.Fatalf("step-up: %d %v", code, stepped)
	}
	if code, body := doJSON(t, h, http.MethodDelete, "/api/workstations/WS-other", userTok, nil); code != http.StatusForbidden {
		t.Fatalf("仅本人不能删除别人的工作站: %d %v", code, body)
	}
	if code, body := doJSON(t, h, http.MethodPatch, "/api/workstations/WS-mine", userTok, map[string]string{"name": "我的站"}); code != http.StatusOK {
		t.Fatalf("应能修改自己的工作站: %d %v", code, body)
	}
	if code, body := doJSON(t, h, http.MethodDelete, "/api/workstations/WS-mine", userTok, nil); code != http.StatusOK {
		t.Fatalf("应能删除自己的工作站: %d %v", code, body)
	}
}

func enrollWS(t *testing.T, h http.Handler, token, wsID string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: wsID},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
	code, body := doJSON(t, h, http.MethodPost, "/api/enrollment/enroll", "", map[string]any{
		"token": token, "workstation_id": wsID, "csr_pem": string(csrPEM),
	})
	if code != http.StatusOK {
		t.Fatalf("enroll %s: %d %v", wsID, code, body)
	}
}

func listWSIDs(t *testing.T, h http.Handler, tok string) []string {
	t.Helper()
	code, body := doJSON(t, h, http.MethodGet, "/api/workstations", tok, nil)
	if code != http.StatusOK {
		t.Fatalf("list workstations: %d %v", code, body)
	}
	items, _ := body["items"].([]any)
	var ids []string
	for _, item := range items {
		m, _ := item.(map[string]any)
		id, _ := m["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func asStrings(v any) []string {
	items, _ := v.([]any)
	var out []string
	for _, item := range items {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}

func memberHas(v any, userID, role string) bool {
	items, _ := v.([]any)
	for _, item := range items {
		m, _ := item.(map[string]any)
		if m["user_id"] == userID && m["role"] == role {
			return true
		}
	}
	return false
}

func wsGrantHas(v any, wsID, role string) bool {
	items, _ := v.([]any)
	for _, item := range items {
		m, _ := item.(map[string]any)
		if m["workstation_id"] == wsID && m["role"] == role {
			return true
		}
	}
	return false
}

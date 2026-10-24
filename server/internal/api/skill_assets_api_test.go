package api_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func patchAssetsRequest(t *testing.T, h http.Handler, token string, revision int, writes map[string][]byte, deletes []string) *httptest.ResponseRecorder {
	t.Helper()
	var data bytes.Buffer
	mw := multipart.NewWriter(&data)
	ops := []map[string]string{}
	for path, content := range writes {
		part := "part-" + path
		ops = append(ops, map[string]string{"path": path, "part": part})
		f, err := mw.CreateFormFile(part, "upload.bin")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	manifest, _ := json.Marshal(map[string]any{"revision": revision, "writes": ops, "deletes": deletes})
	_ = mw.WriteField("manifest", string(manifest))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPatch, "/api/workflow-mcp/skills/asset.api/assets", &data)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestSkillAssetsAPIIncrementalRoundTrip(t *testing.T) {
	h, tok := setupAPI(t)
	md := "---\nname: asset-api\nid: asset.api\nversion: 1.0.0\nextra: preserved # comment\n---\nInstructions\n"
	code, body := doJSON(t, h, "POST", "/api/workflow-mcp/skills", tok, map[string]any{"skill_md": md, "files": []map[string]any{{"path": "scripts/run.py", "content": "print('old')"}, {"path": "delete.txt", "content": "delete"}}})
	if code != 201 {
		t.Fatal(code, body)
	}
	code, index := doJSON(t, h, "GET", "/api/workflow-mcp/skills/asset.api/files", tok, nil)
	if code != 200 || len(index["files"].([]any)) != 3 {
		t.Fatal(code, index)
	}
	for _, raw := range index["files"].([]any) {
		if _, ok := raw.(map[string]any)["content"]; ok {
			t.Fatal("index leaked contents")
		}
	}
	binary := []byte{0, 255, 1, 0, 128}
	rr := patchAssetsRequest(t, h, tok, 1, map[string][]byte{"scripts/run.py": []byte("print('new')"), "assets/image.bin": binary, "empty.txt": {}}, []string{"delete.txt"})
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	read := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/workflow-mcp/skills/asset.api/file?path="+path, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		return res
	}
	if res := read("assets/image.bin"); res.Code != 200 || !bytes.Equal(res.Body.Bytes(), binary) || res.Header().Get("X-Skill-Revision") != "2" {
		t.Fatal("binary corrupt", res.Code, res.Body.Bytes())
	}
	if res := read("SKILL.md"); res.Code != 200 || res.Body.String() != md {
		t.Fatal("main document changed", res.Body.String())
	}
	if res := read("empty.txt"); res.Code != 200 || res.Body.Len() != 0 {
		t.Fatal("empty file missing", res.Code)
	}
	if res := read("delete.txt"); res.Code != 404 {
		t.Fatal("deletion failed", res.Code)
	}
	if res := patchAssetsRequest(t, h, tok, 1, map[string][]byte{"scripts/run.py": []byte("stale")}, nil); res.Code != 409 {
		t.Fatal("stale accepted", res.Code, res.Body.String())
	}
	if res := patchAssetsRequest(t, h, tok, 2, map[string][]byte{"new.txt": []byte("partial"), "../bad": []byte("bad")}, nil); res.Code != 400 {
		t.Fatal("invalid path accepted", res.Code, res.Body.String())
	}
	if res := read("new.txt"); res.Code != 404 {
		t.Fatal("patch not atomic", res.Code)
	}
	if res := patchAssetsRequest(t, h, tok, 2, map[string][]byte{"scripts/run.py": []byte("print('new')")}, nil); res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	} else {
		var response map[string]any
		_ = json.Unmarshal(res.Body.Bytes(), &response)
		if response["changed"] != false || response["revision"] != float64(2) {
			t.Fatal("noop changed revision", response)
		}
	}
	if res := patchAssetsRequest(t, h, "", 2, nil, nil); res.Code != 401 {
		t.Fatal("unauthorized write allowed", res.Code)
	}
	code, exported := doJSON(t, h, "GET", "/api/workflow-mcp/skills/asset.api/export", tok, nil)
	if code != 200 || exported["skill_md"] != md || len(exported["skill"].(map[string]any)["files"].([]any)) != 3 {
		t.Fatal("export mismatch", code, exported)
	}
	// The old editor/client path must no longer erase files when files is omitted.
	code, body = doJSON(t, h, "POST", "/api/workflow-mcp/skills", tok, map[string]any{"skill_md": md})
	if code != 200 {
		t.Fatal(code, body)
	}
	if res := read("assets/image.bin"); res.Code != 200 || !bytes.Equal(res.Body.Bytes(), binary) {
		t.Fatal("legacy update erased assets", res.Code)
	}
}

func TestSkillAssetsViewerReadOnly(t *testing.T) {
	h, tok := setupAPI(t)
	code, body := doJSON(t, h, "POST", "/api/workflow-mcp/skills", tok, map[string]any{"skill_md": "---\nname: viewer-skill\nid: asset.api\n---\nread"})
	if code != 201 {
		t.Fatal(code, body)
	}
	code, body = doJSON(t, h, "POST", "/api/users", tok, map[string]any{"username": "asset-viewer", "password": "viewer12345", "roles": []string{"VIEWER"}})
	if code != 201 {
		t.Fatal(code, body)
	}
	code, body = doJSON(t, h, "POST", "/api/auth/login", "", map[string]string{"username": "asset-viewer", "password": "viewer12345"})
	if code != 200 {
		t.Fatal(code, body)
	}
	viewer := body["token"].(string)
	code, _ = doJSON(t, h, "GET", "/api/workflow-mcp/skills/asset.api/files", viewer, nil)
	if code != 200 {
		t.Fatal("viewer cannot read", code)
	}
	if res := patchAssetsRequest(t, h, viewer, 1, map[string][]byte{"new.txt": []byte("write")}, nil); res.Code != 403 {
		t.Fatal("viewer can mutate", res.Code, res.Body.String())
	}
}

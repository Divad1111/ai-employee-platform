package api_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkflowMCPListRBAC(t *testing.T) {
	h, tok := setupAPI(t)
	code, body := doJSON(t, h, "GET", "/api/workflow-mcp/workflows", tok, nil)
	if code != 200 {
		t.Fatalf("%d %v", code, body)
	}
	yaml := `id: test-wf
name: Test
version: 1.0.0
description: d
skills: []
knowledge: []
steps:
  - id: a
    description: step
`
	code, body = doJSON(t, h, "POST", "/api/workflow-mcp/workflows", tok, map[string]string{"yaml": yaml})
	if code != 201 && code != 200 {
		t.Fatalf("%d %v", code, body)
	}
	code, _ = doJSON(t, h, "GET", "/api/workflow-mcp/workflows", "", nil)
	if code != 401 {
		t.Fatalf("unauth %d", code)
	}

	// GET by ID
	code, getBody := doJSON(t, h, "GET", "/api/workflow-mcp/workflows/test-wf", tok, nil)
	if code != 200 || getBody["workflow"] == nil {
		t.Fatalf("get workflow failed: %d %v", code, getBody)
	}

	// DELETE
	code, _ = doJSON(t, h, "DELETE", "/api/workflow-mcp/workflows/test-wf", tok, nil)
	if code != 204 && code != 200 {
		t.Fatalf("delete workflow failed: %d", code)
	}
}

func TestSkillCRUDAPI(t *testing.T) {
	h, tok := setupAPI(t)

	md := `---
name: api-test-skill
id: skill.api
version: 1.0.0
description: API Test Skill
---
# Guide
Instructions
`
	// 1. Create
	code, body := doJSON(t, h, "POST", "/api/workflow-mcp/skills", tok, map[string]any{
		"content": md,
		"files": []map[string]any{
			{"path": "config.yaml", "content": "env: test"},
		},
	})
	if code != 200 && code != 201 {
		t.Fatalf("create skill failed: %d %v", code, body)
	}

	// 2. List
	code, listBody := doJSON(t, h, "GET", "/api/workflow-mcp/skills", tok, nil)
	if code != 200 {
		t.Fatalf("list skills failed: %d %v", code, listBody)
	}

	// 3. Get
	code, getBody := doJSON(t, h, "GET", "/api/workflow-mcp/skills/skill.api", tok, nil)
	if code != 200 || getBody["skill"] == nil {
		t.Fatalf("get skill failed: %d %v", code, getBody)
	}

	// 4. Export
	code, exportBody := doJSON(t, h, "GET", "/api/workflow-mcp/skills/skill.api/export", tok, nil)
	if code != 200 || exportBody["skill_md"] == nil {
		t.Fatalf("export skill failed: %d %v", code, exportBody)
	}

	// 5. Delete
	code, _ = doJSON(t, h, "DELETE", "/api/workflow-mcp/skills/skill.api", tok, nil)
	if code != 204 && code != 200 {
		t.Fatalf("delete skill failed: %d", code)
	}
}

func TestKnowledgeCRUDAPI(t *testing.T) {
	h, tok := setupAPI(t)

	md := `---
title: System Overview
namespace: architecture
---
# Overview
This is a test architecture doc.
`
	// 1. Create
	code, body := doJSON(t, h, "POST", "/api/workflow-mcp/knowledge", tok, map[string]any{
		"path":    "architecture/overview.md",
		"content": md,
	})
	if code != 200 && code != 201 {
		t.Fatalf("create knowledge failed: %d %v", code, body)
	}
	docID := ""
	if docMap, ok := body["doc"].(map[string]any); ok {
		docID = docMap["id"].(string)
	}

	// 2. List
	code, listBody := doJSON(t, h, "GET", "/api/workflow-mcp/knowledge", tok, nil)
	if code != 200 {
		t.Fatalf("list knowledge failed: %d %v", code, listBody)
	}

	// 3. Search
	code, searchBody := doJSON(t, h, "GET", "/api/workflow-mcp/knowledge/search?q=architecture", tok, nil)
	if code != 200 {
		t.Fatalf("search knowledge failed: %d %v", code, searchBody)
	}

	// 4. Reindex
	code, reindexBody := doJSON(t, h, "POST", "/api/workflow-mcp/knowledge/reindex", tok, nil)
	if code != 200 {
		t.Fatalf("reindex knowledge failed: %d %v", code, reindexBody)
	}

	// 5. Delete
	if docID != "" {
		code, _ = doJSON(t, h, "DELETE", "/api/workflow-mcp/knowledge/"+docID, tok, nil)
		if code != 204 && code != 200 {
			t.Fatalf("delete knowledge failed: %d", code)
		}
	}
}

func TestUnifiedSearchAPI(t *testing.T) {
	h, tok := setupAPI(t)
	code, body := doJSON(t, h, "GET", "/api/workflow-mcp/search?q=test", tok, nil)
	if code != 200 {
		t.Fatalf("unified search failed: %d %v", code, body)
	}
}

func TestEmployeeWorkflowGrantAndTokenAPI(t *testing.T) {
	h, tok := setupAPI(t)

	// 1. 创建员工
	code, emp := doJSON(t, h, "POST", "/api/employees", tok, map[string]string{"name": "GrantDev"})
	if code != 201 {
		t.Fatalf("create employee failed: %d %v", code, emp)
	}
	empID := emp["id"].(string)

	// 2. 创建工作流
	wfYAML := `id: wf-for-grant
name: For Grant
version: 1.0.0
steps:
  - id: s1
    description: d
`
	code, _ = doJSON(t, h, "POST", "/api/workflow-mcp/workflows", tok, map[string]string{"yaml": wfYAML})
	if code != 201 && code != 200 {
		t.Fatal("create workflow failed")
	}

	// 3. 授权工作流
	code, grantRes := doJSON(t, h, "POST", fmt.Sprintf("/api/employees/%s/workflows", empID), tok, map[string]string{
		"workflow_id": "wf-for-grant",
	})
	if code != 200 && code != 201 {
		t.Fatalf("grant workflow failed: %d %v", code, grantRes)
	}

	// 4. 查询员工工作流
	code, listWf := doJSON(t, h, "GET", fmt.Sprintf("/api/employees/%s/workflows", empID), tok, nil)
	if code != 200 {
		t.Fatalf("list employee workflows failed: %d %v", code, listWf)
	}

	// 5. 签发 MCP Token
	code, tokRes := doJSON(t, h, "POST", fmt.Sprintf("/api/employees/%s/mcp-tokens", empID), tok, map[string]any{
		"label":            "test-token",
		"expires_in_hours": 24,
	})
	if code != 200 && code != 201 {
		t.Fatalf("issue token failed: %d %v", code, tokRes)
	}
	var tokenID string
	if tMap, ok := tokRes["token"].(map[string]any); ok {
		tokenID = tMap["id"].(string)
	}

	// 6. 查询员工 MCP Tokens
	code, listToks := doJSON(t, h, "GET", fmt.Sprintf("/api/employees/%s/mcp-tokens", empID), tok, nil)
	if code != 200 {
		t.Fatalf("list employee tokens failed: %d %v", code, listToks)
	}

	// 7. 吊销 MCP Token
	if tokenID != "" {
		code, _ = doJSON(t, h, "DELETE", fmt.Sprintf("/api/mcp-tokens/%s", tokenID), tok, nil)
		if code != 204 && code != 200 {
			t.Fatalf("revoke token failed: %d", code)
		}
	}

	// 8. 撤销工作流授权
	code, _ = doJSON(t, h, "DELETE", fmt.Sprintf("/api/employees/%s/workflows?workflow_id=wf-for-grant", empID), tok, nil)
	if code != 204 && code != 200 {
		t.Fatalf("revoke workflow grant failed: %d", code)
	}
}

func TestImportWorkflowMCPZipAPI(t *testing.T) {
	h, tok := setupAPI(t)

	// 构造内存 Zip 包
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)

	// 添加工作流
	wfFile, err := zw.Create("workflows/zip-wf.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = wfFile.Write([]byte(`id: zip-wf
name: Zip Workflow
version: 1.0.0
steps:
  - id: s1
    description: d
`))

	// 添加技能
	skillMD, err := zw.Create("skills/zip-skill/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = skillMD.Write([]byte(`---
name: zip-skill
id: skill.zip
version: 1.0.0
---
instructions
`))

	// 添加技能附带文件
	skillHelper, err := zw.Create("skills/zip-skill/helpers/util.py")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = skillHelper.Write([]byte("def helper(): pass"))

	// 添加知识库
	knowDoc, err := zw.Create("knowledge/common/test.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = knowDoc.Write([]byte(`---
title: Zip Knowledge
namespace: common
---
Content
`))

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	// 构造 multipart 请求
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "data.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(zipBuf.Bytes())
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/workflow-mcp/import", &body)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("import failed: %d %s", rr.Code, rr.Body.String())
	}
	var res map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	stats, ok := res["stats"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response structure: %v", res)
	}
	if stats["workflows"] != float64(1) || stats["skills"] != float64(1) || stats["knowledge"] != float64(1) {
		t.Fatalf("expected 1 of each in stats: %+v", stats)
	}
}

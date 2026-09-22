package workflowmcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Service 工作流MCP 业务服务。
type Service struct {
	store Store
}

// NewService 创建服务。
func NewService(store Store) *Service {
	return &Service{store: store}
}

// Store 暴露底层存储（导入/测试用）。
func (s *Service) Store() Store { return s.store }

// ---------- Workflow ----------

// ListWorkflows 列出全部工作流。
func (s *Service) ListWorkflows(ctx context.Context) ([]*Workflow, error) {
	return s.store.ListWorkflows(ctx)
}

// GetWorkflow 按 id 获取。
func (s *Service) GetWorkflow(ctx context.Context, id string) (*Workflow, error) {
	return s.store.GetWorkflow(ctx, id)
}

// UpsertWorkflowYAML 从 YAML 内容 upsert 工作流。
func (s *Service) UpsertWorkflowYAML(ctx context.Context, content, bump string) (*Workflow, string, error) {
	w, err := ParseWorkflowYAML(content)
	if err != nil {
		return nil, "", err
	}
	return s.UpsertWorkflow(ctx, w, bump)
}

// UpsertWorkflow upsert 工作流并处理版本。
func (s *Service) UpsertWorkflow(ctx context.Context, w *Workflow, bump string) (*Workflow, string, error) {
	if w == nil || w.ID == "" {
		return nil, "", fmt.Errorf("%w: 缺少 id", ErrInvalidInput)
	}
	if bump == "" {
		bump = "patch"
	}
	existing, err := s.store.GetWorkflow(ctx, w.ID)
	isNew := err == ErrNotFound
	if err != nil && !isNew {
		return nil, "", err
	}
	action := "updated"
	if isNew {
		action = "created"
		w.Version = ResolveSavedVersion("", w.Version, true, bump)
	} else {
		w.Version = ResolveSavedVersion(existing.Version, w.Version, false, bump)
	}
	if w.Status == "" {
		w.Status = "active"
	}
	if w.Approval == nil {
		w.Approval = map[string]bool{}
	}
	if err := s.store.UpsertWorkflow(ctx, w); err != nil {
		return nil, "", err
	}
	out, err := s.store.GetWorkflow(ctx, w.ID)
	return out, action, err
}

// DeleteWorkflow 删除工作流。
func (s *Service) DeleteWorkflow(ctx context.Context, id string) error {
	return s.store.DeleteWorkflow(ctx, id)
}

// SearchWorkflows 内存 token overlap 搜索。
func (s *Service) SearchWorkflows(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 10
	}
	list, err := s.store.ListWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	var hits []SearchHit
	for _, w := range list {
		text := strings.Join([]string{w.ID, w.Name, w.Description, strings.Join(w.WhenToUse, " ")}, " ")
		score := ScoreText(query, text)
		if score > 0 {
			hits = append(hits, SearchHit{ID: w.ID, Name: w.Name, Version: w.Version, Description: w.Description, Score: score})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// WorkflowToYAML 导出工作流为 YAML。
func WorkflowToYAML(w *Workflow) (string, error) {
	type stepOut struct {
		ID          string `yaml:"id"`
		Description string `yaml:"description"`
	}
	steps := make([]stepOut, 0, len(w.Steps))
	for _, st := range w.Steps {
		steps = append(steps, stepOut{ID: st.ID, Description: st.Description})
	}
	m := map[string]any{
		"id":          w.ID,
		"name":        w.Name,
		"version":     w.Version,
		"description": w.Description,
		"when_to_use": w.WhenToUse,
		"inputs":      w.Inputs,
		"skills":      w.SkillRefs,
		"knowledge":   w.KnowledgeRefs,
		"steps":       steps,
		"approval":    w.Approval,
	}
	for k, v := range w.Extra {
		m[k] = v
	}
	b, err := yaml.Marshal(m)
	return string(b), err
}

// ParseWorkflowYAML 解析工作流 YAML。
func ParseWorkflowYAML(content string) (*Workflow, error) {
	raw := map[string]any{}
	if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	w := &Workflow{
		WhenToUse:     []string{},
		Inputs:        []string{},
		SkillRefs:     []string{},
		KnowledgeRefs: []string{},
		Steps:         []WorkflowStep{},
		Approval:      map[string]bool{},
		Extra:         map[string]any{},
		Status:        "active",
		Version:       "1.0.0",
	}
	known := map[string]bool{
		"id": true, "name": true, "version": true, "description": true,
		"when_to_use": true, "inputs": true, "skills": true, "knowledge": true,
		"steps": true, "approval": true, "status": true,
	}
	if v, ok := raw["id"].(string); ok {
		w.ID = v
	}
	if v, ok := raw["name"].(string); ok {
		w.Name = v
	}
	if v, ok := raw["version"].(string); ok && v != "" {
		w.Version = v
	}
	if v, ok := raw["description"].(string); ok {
		w.Description = v
	}
	if v, ok := raw["status"].(string); ok {
		w.Status = v
	}
	w.WhenToUse = toStringSlice(raw["when_to_use"])
	w.Inputs = toStringSlice(raw["inputs"])
	w.SkillRefs = toStringSlice(raw["skills"])
	w.KnowledgeRefs = toStringSlice(raw["knowledge"])
	if steps, ok := raw["steps"].([]any); ok {
		for _, s := range steps {
			sm, ok := s.(map[string]any)
			if !ok {
				continue
			}
			st := WorkflowStep{}
			if v, ok := sm["id"].(string); ok {
				st.ID = v
			}
			if v, ok := sm["description"].(string); ok {
				st.Description = v
			}
			w.Steps = append(w.Steps, st)
		}
	}
	if ap, ok := raw["approval"].(map[string]any); ok {
		for k, v := range ap {
			if b, ok := v.(bool); ok {
				w.Approval[k] = b
			}
		}
	}
	for k, v := range raw {
		if !known[k] {
			w.Extra[k] = v
		}
	}
	if w.ID == "" {
		return nil, fmt.Errorf("%w: 缺少 id", ErrInvalidInput)
	}
	return w, nil
}

func toStringSlice(v any) []string {
	if v == nil {
		return []string{}
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return []string{}
	}
}

// ---------- Skill ----------

// ListSkills 列出技能包摘要。
func (s *Service) ListSkills(ctx context.Context) ([]*SkillPackage, error) {
	return s.store.ListSkills(ctx)
}

// GetSkill 获取技能包（不含文件内容）。
func (s *Service) GetSkill(ctx context.Context, id string) (*SkillPackage, error) {
	sp, err := s.store.GetSkill(ctx, id)
	if err != nil {
		return nil, err
	}
	files, _ := s.store.GetSkillFiles(ctx, sp.ID)
	// 仅返回索引
	idx := make([]SkillFile, len(files))
	for i, f := range files {
		idx[i] = SkillFile{Path: f.Path, SHA256: f.SHA256, Size: f.Size}
	}
	sp.Files = idx
	return sp, nil
}

// ExportSkill 导出完整技能包（含文件内容 base64）。
func (s *Service) ExportSkill(ctx context.Context, id string) (*SkillPackage, string, error) {
	sp, err := s.store.GetSkill(ctx, id)
	if err != nil {
		return nil, "", err
	}
	files, err := s.store.GetSkillFiles(ctx, sp.ID)
	if err != nil {
		return nil, "", err
	}
	outFiles := make([]SkillFile, len(files))
	for i, f := range files {
		enc := "utf-8"
		content := string(f.Content)
		if !isPrintableUTF8(f.Content) {
			enc = "base64"
			content = base64.StdEncoding.EncodeToString(f.Content)
		}
		outFiles[i] = SkillFile{
			Path: f.Path, SHA256: f.SHA256, Size: f.Size,
			Encoding: enc, ContentB64: content, Content: f.Content,
		}
	}
	sp.Files = outFiles
	md, err := RenderSkillMD(sp)
	return sp, md, err
}

// UpsertSkillMD 从 SKILL.md + 文件列表 upsert。
func (s *Service) UpsertSkillMD(ctx context.Context, skillMD string, files []SkillFile, bump string) (*SkillPackage, string, error) {
	sp, err := ParseSkillMD(skillMD)
	if err != nil {
		return nil, "", err
	}
	return s.UpsertSkill(ctx, sp, files, bump)
}

// UpsertSkill upsert 技能包。
func (s *Service) UpsertSkill(ctx context.Context, sp *SkillPackage, files []SkillFile, bump string) (*SkillPackage, string, error) {
	if sp == nil || sp.ID == "" {
		return nil, "", fmt.Errorf("%w: 缺少 id", ErrInvalidInput)
	}
	if bump == "" {
		bump = "patch"
	}
	if sp.CursorName == "" {
		sp.CursorName = CursorSkillName(sp.ID)
	}
	existing, err := s.store.GetSkill(ctx, sp.ID)
	isNew := err == ErrNotFound
	if err != nil && !isNew {
		return nil, "", err
	}
	action := "updated"
	if isNew {
		action = "created"
		sp.Version = ResolveSavedVersion("", sp.Version, true, bump)
	} else {
		sp.Version = ResolveSavedVersion(existing.Version, sp.Version, false, bump)
	}
	// 规范化文件
	norm := make([]SkillFile, 0, len(files))
	for _, f := range files {
		if f.Path == "" {
			continue
		}
		content := f.Content
		if len(content) == 0 && f.ContentB64 != "" {
			if f.Encoding == "base64" {
				content, _ = base64.StdEncoding.DecodeString(f.ContentB64)
			} else {
				content = []byte(f.ContentB64)
			}
		}
		sum := sha256.Sum256(content)
		norm = append(norm, SkillFile{
			Path: f.Path, Content: content,
			SHA256: hex.EncodeToString(sum[:]), Size: int64(len(content)),
		})
	}
	sp.ContentHash = ComputeContentHash(sp, norm)
	if err := s.store.UpsertSkill(ctx, sp, norm); err != nil {
		return nil, "", err
	}
	out, err := s.store.GetSkill(ctx, sp.ID)
	return out, action, err
}

// DeleteSkill 删除技能包。
func (s *Service) DeleteSkill(ctx context.Context, id string) error {
	sp, err := s.store.GetSkill(ctx, id)
	if err != nil {
		return err
	}
	return s.store.DeleteSkill(ctx, sp.ID)
}

// SearchSkills 搜索技能。
func (s *Service) SearchSkills(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 10
	}
	list, err := s.store.ListSkills(ctx)
	if err != nil {
		return nil, err
	}
	var hits []SearchHit
	for _, sp := range list {
		text := strings.Join([]string{sp.ID, sp.Name, sp.CursorName, sp.Description, sp.Body}, " ")
		score := ScoreText(query, text)
		if score > 0 {
			hits = append(hits, SearchHit{
				ID: sp.ID, Name: sp.Name, Version: sp.Version,
				Description: sp.Description, CursorName: sp.CursorName, Score: score,
			})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// CheckSkill 对比本地版本与 MCP 版本。
func (s *Service) CheckSkill(ctx context.Context, id, localVersion string) (map[string]any, error) {
	sp, err := s.store.GetSkill(ctx, id)
	if err != nil {
		return map[string]any{
			"id": id, "status": "missing_on_mcp", "message": "MCP 中不存在该技能",
		}, nil
	}
	status := "current"
	action := "none"
	msg := "已是最新"
	if localVersion == "" {
		status = "missing"
		action = "install"
		msg = "本地未安装"
	} else {
		cmp := CompareVersions(localVersion, sp.Version)
		if cmp < 0 {
			status = "outdated"
			action = "update"
			msg = "本地版本落后"
		} else if cmp > 0 {
			status = "newer_local"
			action = "none"
			msg = "本地版本更新"
		}
	}
	result := map[string]any{
		"id": sp.ID, "cursor_name": sp.CursorName,
		"mcp_version": sp.Version, "local_version": localVersion,
		"status": status, "action": action, "message": msg,
		"install_to": "~/.cursor/skills/" + sp.CursorName + "/",
	}
	if status == "missing" || status == "outdated" {
		exported, md, err := s.ExportSkill(ctx, sp.ID)
		if err == nil {
			result["skill_md"] = md
			result["files"] = exported.Files
		}
	}
	return result, nil
}

// CheckLocalSkills 根据客户端上报的本地技能清单做对比。
// localSkills: [{id, cursor_name, version}, ...]
func (s *Service) CheckLocalSkills(ctx context.Context, localSkills []map[string]string) (map[string]any, error) {
	mcpList, err := s.store.ListSkills(ctx)
	if err != nil {
		return nil, err
	}
	localByKey := map[string]map[string]string{}
	for _, ls := range localSkills {
		key := ls["id"]
		if key == "" {
			key = ls["cursor_name"]
		}
		if key != "" {
			localByKey[key] = ls
			if cn := ls["cursor_name"]; cn != "" {
				localByKey[cn] = ls
			}
		}
	}
	var results []map[string]any
	counts := map[string]int{"missing": 0, "outdated": 0, "current": 0, "newer_local": 0}
	seen := map[string]bool{}
	for _, sp := range mcpList {
		ls := localByKey[sp.ID]
		if ls == nil {
			ls = localByKey[sp.CursorName]
		}
		localVer := ""
		if ls != nil {
			localVer = ls["version"]
			seen[sp.ID] = true
			seen[sp.CursorName] = true
		}
		r, _ := s.CheckSkill(ctx, sp.ID, localVer)
		results = append(results, r)
		if st, ok := r["status"].(string); ok {
			counts[st]++
		}
	}
	var localOnly []map[string]string
	for _, ls := range localSkills {
		key := ls["id"]
		if key == "" {
			key = ls["cursor_name"]
		}
		if key != "" && !seen[key] && !seen[ls["cursor_name"]] {
			localOnly = append(localOnly, ls)
		}
	}
	return map[string]any{
		"mcp_skill_count":  len(mcpList),
		"local_only_count": len(localOnly),
		"counts":           counts,
		"skills":           results,
		"local_only":       localOnly,
	}, nil
}

// CheckWorkflowSkills 检查工作流依赖的技能。
func (s *Service) CheckWorkflowSkills(ctx context.Context, workflowID string, localSkills map[string]string) (map[string]any, error) {
	w, err := s.store.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, sid := range w.SkillRefs {
		localVer := localSkills[sid]
		r, _ := s.CheckSkill(ctx, sid, localVer)
		results = append(results, r)
	}
	return map[string]any{
		"workflow_id":      w.ID,
		"workflow_version": w.Version,
		"skills":           results,
	}, nil
}

// ComputeContentHash 计算技能包内容哈希。
func ComputeContentHash(sp *SkillPackage, files []SkillFile) string {
	h := sha256.New()
	md, _ := RenderSkillMD(sp)
	h.Write([]byte(md))
	sorted := append([]SkillFile(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	for _, f := range sorted {
		h.Write([]byte(f.Path))
		h.Write(f.Content)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func isPrintableUTF8(b []byte) bool {
	s := string(b)
	for _, r := range s {
		if r == 0xFFFD {
			return false
		}
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}

// ---------- Knowledge ----------

// ListKnowledge 列出知识文档。
func (s *Service) ListKnowledge(ctx context.Context, namespace string) ([]*KnowledgeDoc, error) {
	return s.store.ListKnowledge(ctx, namespace)
}

// GetKnowledge 获取知识文档。
func (s *Service) GetKnowledge(ctx context.Context, id string) (*KnowledgeDoc, error) {
	return s.store.GetKnowledge(ctx, id)
}

// UpsertKnowledgeMD 从 Markdown upsert 知识。
func (s *Service) UpsertKnowledgeMD(ctx context.Context, path, content, bump string) (*KnowledgeDoc, string, error) {
	doc, err := ParseKnowledgeMD(path, content)
	if err != nil {
		return nil, "", err
	}
	return s.UpsertKnowledge(ctx, doc, bump)
}

// UpsertKnowledge upsert 知识并重建分块索引。
func (s *Service) UpsertKnowledge(ctx context.Context, doc *KnowledgeDoc, bump string) (*KnowledgeDoc, string, error) {
	if doc == nil || doc.ID == "" {
		return nil, "", fmt.Errorf("%w: 缺少 id", ErrInvalidInput)
	}
	if bump == "" {
		bump = "patch"
	}
	existing, err := s.store.GetKnowledge(ctx, doc.ID)
	isNew := err == ErrNotFound
	if err != nil && !isNew {
		return nil, "", err
	}
	action := "updated"
	if isNew {
		action = "created"
		doc.Version = ResolveSavedVersion("", doc.Version, true, bump)
	} else {
		doc.Version = ResolveSavedVersion(existing.Version, doc.Version, false, bump)
	}
	if err := s.store.UpsertKnowledge(ctx, doc); err != nil {
		return nil, "", err
	}
	chunks := ChunkKnowledge(doc)
	if err := s.store.ReplaceChunks(ctx, doc.ID, chunks); err != nil {
		return nil, "", err
	}
	out, err := s.store.GetKnowledge(ctx, doc.ID)
	return out, action, err
}

// DeleteKnowledge 删除知识文档。
func (s *Service) DeleteKnowledge(ctx context.Context, id string) error {
	return s.store.DeleteKnowledge(ctx, id)
}

// SearchKnowledge 全文检索。
func (s *Service) SearchKnowledge(ctx context.Context, query, namespace string, limit int) ([]KnowledgeHit, error) {
	return s.store.SearchKnowledge(ctx, query, namespace, limit)
}

// SearchCases 检索 cases 命名空间。
func (s *Service) SearchCases(ctx context.Context, query string, limit int) ([]KnowledgeHit, error) {
	return s.store.SearchKnowledge(ctx, query, "cases", limit)
}

// ReindexKnowledge 重建全部知识检索索引。
func (s *Service) ReindexKnowledge(ctx context.Context) (int, error) {
	ids, err := s.store.ListAllDocIDs(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		doc, err := s.store.GetKnowledge(ctx, id)
		if err != nil {
			continue
		}
		chunks := ChunkKnowledge(doc)
		if err := s.store.ReplaceChunks(ctx, id, chunks); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

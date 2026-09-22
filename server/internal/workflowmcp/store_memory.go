package workflowmcp

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemoryStore 内存实现（测试与无 DB 回退）。
type MemoryStore struct {
	mu        sync.RWMutex
	workflows map[string]*Workflow
	skills    map[string]*SkillPackage
	skillFiles map[string][]SkillFile // skillID -> files
	knowledge map[string]*KnowledgeDoc
	aliases   map[string]string // alias -> docID
	chunks    map[string][]KnowledgeChunk
	grants    map[string]map[string]EmployeeGrant // employeeID -> workflowID -> grant
}

// NewMemoryStore 创建空内存存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		workflows:  map[string]*Workflow{},
		skills:     map[string]*SkillPackage{},
		skillFiles: map[string][]SkillFile{},
		knowledge:  map[string]*KnowledgeDoc{},
		aliases:    map[string]string{},
		chunks:     map[string][]KnowledgeChunk{},
		grants:     map[string]map[string]EmployeeGrant{},
	}
}

func (m *MemoryStore) ListWorkflows(_ context.Context) ([]*Workflow, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Workflow, 0, len(m.workflows))
	for _, w := range m.workflows {
		cp := *w
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemoryStore) GetWorkflow(_ context.Context, id string) (*Workflow, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w, ok := m.workflows[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *w
	return &cp, nil
}

func (m *MemoryStore) UpsertWorkflow(_ context.Context, w *Workflow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if existing, ok := m.workflows[w.ID]; ok {
		w.CreatedAt = existing.CreatedAt
	} else {
		w.CreatedAt = now
	}
	w.UpdatedAt = now
	cp := *w
	m.workflows[w.ID] = &cp
	return nil
}

func (m *MemoryStore) DeleteWorkflow(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workflows[id]; !ok {
		return ErrNotFound
	}
	delete(m.workflows, id)
	for emp, gs := range m.grants {
		delete(gs, id)
		if len(gs) == 0 {
			delete(m.grants, emp)
		}
	}
	return nil
}

func (m *MemoryStore) ListSkills(_ context.Context) ([]*SkillPackage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*SkillPackage, 0, len(m.skills))
	for _, s := range m.skills {
		cp := *s
		cp.Files = nil
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemoryStore) GetSkill(_ context.Context, idOrCursor string) (*SkillPackage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.skills[idOrCursor]; ok {
		cp := *s
		return &cp, nil
	}
	for _, s := range m.skills {
		if s.CursorName == idOrCursor {
			cp := *s
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemoryStore) GetSkillFiles(_ context.Context, skillID string) ([]SkillFile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	files := m.skillFiles[skillID]
	out := make([]SkillFile, len(files))
	copy(out, files)
	return out, nil
}

func (m *MemoryStore) UpsertSkill(_ context.Context, sp *SkillPackage, files []SkillFile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if existing, ok := m.skills[sp.ID]; ok {
		sp.CreatedAt = existing.CreatedAt
	} else {
		sp.CreatedAt = now
	}
	sp.UpdatedAt = now
	cp := *sp
	cp.Files = nil
	m.skills[sp.ID] = &cp
	copied := make([]SkillFile, len(files))
	for i, f := range files {
		copied[i] = f
		if f.Content != nil {
			copied[i].Content = append([]byte(nil), f.Content...)
		}
	}
	m.skillFiles[sp.ID] = copied
	return nil
}

func (m *MemoryStore) DeleteSkill(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.skills[id]; !ok {
		return ErrNotFound
	}
	delete(m.skills, id)
	delete(m.skillFiles, id)
	return nil
}

func (m *MemoryStore) ListKnowledge(_ context.Context, namespace string) ([]*KnowledgeDoc, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*KnowledgeDoc, 0)
	for _, d := range m.knowledge {
		if namespace != "" && d.Namespace != namespace {
			continue
		}
		cp := *d
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemoryStore) GetKnowledge(_ context.Context, idOrAlias string) (*KnowledgeDoc, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if d, ok := m.knowledge[idOrAlias]; ok {
		cp := *d
		return &cp, nil
	}
	if id, ok := m.aliases[idOrAlias]; ok {
		if d, ok := m.knowledge[id]; ok {
			cp := *d
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemoryStore) UpsertKnowledge(_ context.Context, doc *KnowledgeDoc) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if existing, ok := m.knowledge[doc.ID]; ok {
		doc.CreatedAt = existing.CreatedAt
		for _, a := range existing.Aliases {
			delete(m.aliases, a)
		}
	} else {
		doc.CreatedAt = now
	}
	doc.UpdatedAt = now
	cp := *doc
	m.knowledge[doc.ID] = &cp
	for _, a := range doc.Aliases {
		m.aliases[a] = doc.ID
	}
	return nil
}

func (m *MemoryStore) DeleteKnowledge(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.knowledge[id]
	if !ok {
		return ErrNotFound
	}
	for _, a := range d.Aliases {
		delete(m.aliases, a)
	}
	delete(m.knowledge, id)
	delete(m.chunks, id)
	return nil
}

func (m *MemoryStore) ReplaceChunks(_ context.Context, docID string, chunks []KnowledgeChunk) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]KnowledgeChunk, len(chunks))
	copy(copied, chunks)
	m.chunks[docID] = copied
	return nil
}

func (m *MemoryStore) SearchKnowledge(_ context.Context, query, namespace string, limit int) ([]KnowledgeHit, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 {
		limit = 10
	}
	type scored struct {
		hit   KnowledgeHit
		score float64
	}
	var hits []scored
	for docID, chunks := range m.chunks {
		doc, ok := m.knowledge[docID]
		if !ok {
			continue
		}
		if namespace != "" && doc.Namespace != namespace {
			continue
		}
		best := 0.0
		bestContent := ""
		for _, c := range chunks {
			s := ScoreText(query, c.Heading+" "+c.Content)
			if s > best {
				best = s
				bestContent = c.Content
			}
		}
		if best > 0 {
			hits = append(hits, scored{hit: KnowledgeHit{
				ID: doc.ID, Title: doc.Title, Source: doc.Source, Path: doc.Path,
				Score: best, Content: bestContent, Metadata: doc.Metadata,
			}, score: best})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]KnowledgeHit, len(hits))
	for i, h := range hits {
		out[i] = h.hit
	}
	return out, nil
}

func (m *MemoryStore) ListAllDocIDs(_ context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.knowledge))
	for id := range m.knowledge {
		out = append(out, id)
	}
	return out, nil
}

func (m *MemoryStore) ListGrantsByEmployee(_ context.Context, employeeID string) ([]EmployeeGrant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	gs := m.grants[employeeID]
	out := make([]EmployeeGrant, 0, len(gs))
	for _, g := range gs {
		out = append(out, g)
	}
	return out, nil
}

func (m *MemoryStore) ListEmployeesByWorkflow(_ context.Context, workflowID string) ([]EmployeeGrant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []EmployeeGrant
	for _, gs := range m.grants {
		if g, ok := gs[workflowID]; ok {
			out = append(out, g)
		}
	}
	return out, nil
}

func (m *MemoryStore) GrantWorkflow(_ context.Context, g EmployeeGrant) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workflows[g.WorkflowID]; !ok {
		return ErrNotFound
	}
	if g.GrantedAt.IsZero() {
		g.GrantedAt = time.Now().UTC()
	}
	if m.grants[g.EmployeeID] == nil {
		m.grants[g.EmployeeID] = map[string]EmployeeGrant{}
	}
	m.grants[g.EmployeeID][g.WorkflowID] = g
	return nil
}

func (m *MemoryStore) RevokeWorkflow(_ context.Context, employeeID, workflowID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	gs := m.grants[employeeID]
	if gs == nil {
		return ErrNotFound
	}
	if _, ok := gs[workflowID]; !ok {
		return ErrNotFound
	}
	delete(gs, workflowID)
	return nil
}

// Ensure MemoryStore implements Store.
var _ Store = (*MemoryStore)(nil)

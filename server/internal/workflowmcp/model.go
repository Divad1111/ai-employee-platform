package workflowmcp

import (
	"time"
)

// WorkflowStep 工作流步骤。
type WorkflowStep struct {
	ID          string         `json:"id" yaml:"id"`
	Description string         `json:"description" yaml:"description"`
	Extra       map[string]any `json:"-" yaml:",inline"`
}

// Workflow 工作流定义（权限主体）。
type Workflow struct {
	ID            string            `json:"id" yaml:"id"`
	Name          string            `json:"name" yaml:"name"`
	Version       string            `json:"version" yaml:"version"`
	Description   string            `json:"description" yaml:"description"`
	WhenToUse     []string          `json:"when_to_use" yaml:"when_to_use"`
	Inputs        []string          `json:"inputs" yaml:"inputs"`
	SkillRefs     []string          `json:"skills" yaml:"skills"`
	KnowledgeRefs []string          `json:"knowledge" yaml:"knowledge"`
	Steps         []WorkflowStep    `json:"steps" yaml:"steps"`
	Approval      map[string]bool   `json:"approval" yaml:"approval"`
	Extra         map[string]any    `json:"extra,omitempty" yaml:"-"`
	Status        string            `json:"status" yaml:"status"`
	CreatedAt     time.Time         `json:"created_at" yaml:"-"`
	UpdatedAt     time.Time         `json:"updated_at" yaml:"-"`
}

// SkillFile 技能包附属文件。
type SkillFile struct {
	Path     string `json:"path"`
	Content  []byte `json:"-"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size_bytes"`
	Encoding string `json:"encoding,omitempty"` // utf-8 | base64（导出时使用）
	// ContentB64 仅用于 JSON 导出。
	ContentB64 string `json:"content,omitempty"`
}

// SkillPackage Cursor 兼容技能包。
type SkillPackage struct {
	ID                      string         `json:"id"`
	CursorName              string         `json:"cursor_name"`
	Name                    string         `json:"name"`
	Version                 string         `json:"version"`
	Description             string         `json:"description"`
	Body                    string         `json:"body"`
	Metadata                map[string]any `json:"metadata"`
	DisableModelInvocation  bool           `json:"disable_model_invocation"`
	ContentHash             string         `json:"content_hash"`
	Files                   []SkillFile    `json:"files,omitempty"`
	CreatedAt               time.Time      `json:"created_at"`
	UpdatedAt               time.Time      `json:"updated_at"`
}

// KnowledgeDoc 知识文档。
type KnowledgeDoc struct {
	ID        string         `json:"id"`
	Path      string         `json:"path"`
	Namespace string         `json:"namespace"`
	Title     string         `json:"title"`
	Source    string         `json:"source"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata"`
	Aliases   []string       `json:"aliases"`
	Version   string         `json:"version"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// KnowledgeChunk 知识分块（用于全文检索）。
type KnowledgeChunk struct {
	DocID      string
	ChunkIndex int
	Heading    string
	Content    string
	Tokens     string
}

// KnowledgeHit 检索命中。
type KnowledgeHit struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Source   string         `json:"source"`
	Path     string         `json:"path"`
	Score    float64        `json:"score"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata"`
}

// EmployeeGrant 员工工作流授权。
type EmployeeGrant struct {
	EmployeeID string    `json:"employee_id"`
	WorkflowID string    `json:"workflow_id"`
	GrantedBy  string    `json:"granted_by"`
	GrantedAt  time.Time `json:"granted_at"`
}

// Scope 员工可见范围（工作流授权闭包）。
type Scope struct {
	WorkflowIDs       []string `json:"workflow_ids"`
	SkillIDs          []string `json:"skill_ids"`
	KnowledgePrefixes []string `json:"knowledge_prefixes"`
	AllKnowledge      bool     `json:"all_knowledge"`
}

// SearchHit 通用搜索命中（工作流/技能内存打分）。
type SearchHit struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Version     string  `json:"version"`
	Description string  `json:"description"`
	CursorName  string  `json:"cursor_name,omitempty"`
	Score       float64 `json:"score"`
}

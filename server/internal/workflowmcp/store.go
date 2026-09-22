package workflowmcp

import (
	"context"
	"errors"
)

// 领域错误。
var (
	ErrNotFound      = errors.New("资源不存在")
	ErrInvalidInput  = errors.New("参数无效")
	ErrConflict      = errors.New("资源冲突")
	ErrUnauthorized  = errors.New("未授权")
)

// Store 工作流MCP 持久化接口。
type Store interface {
	// Workflows
	ListWorkflows(ctx context.Context) ([]*Workflow, error)
	GetWorkflow(ctx context.Context, id string) (*Workflow, error)
	UpsertWorkflow(ctx context.Context, w *Workflow) error
	DeleteWorkflow(ctx context.Context, id string) error

	// Skills
	ListSkills(ctx context.Context) ([]*SkillPackage, error)
	GetSkill(ctx context.Context, idOrCursor string) (*SkillPackage, error)
	GetSkillFiles(ctx context.Context, skillID string) ([]SkillFile, error)
	UpsertSkill(ctx context.Context, sp *SkillPackage, files []SkillFile) error
	DeleteSkill(ctx context.Context, id string) error

	// Knowledge
	ListKnowledge(ctx context.Context, namespace string) ([]*KnowledgeDoc, error)
	GetKnowledge(ctx context.Context, idOrAlias string) (*KnowledgeDoc, error)
	UpsertKnowledge(ctx context.Context, doc *KnowledgeDoc) error
	DeleteKnowledge(ctx context.Context, id string) error
	ReplaceChunks(ctx context.Context, docID string, chunks []KnowledgeChunk) error
	SearchKnowledge(ctx context.Context, query, namespace string, limit int) ([]KnowledgeHit, error)
	ListAllDocIDs(ctx context.Context) ([]string, error)

	// Grants
	ListGrantsByEmployee(ctx context.Context, employeeID string) ([]EmployeeGrant, error)
	ListEmployeesByWorkflow(ctx context.Context, workflowID string) ([]EmployeeGrant, error)
	GrantWorkflow(ctx context.Context, g EmployeeGrant) error
	RevokeWorkflow(ctx context.Context, employeeID, workflowID string) error
}

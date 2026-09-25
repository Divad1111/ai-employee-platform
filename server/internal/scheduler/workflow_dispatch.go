package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/mcp"
	"github.com/ai-employee-platform/server/internal/mcpauth"
	"github.com/ai-employee-platform/server/internal/workflowmcp"
)

// FullCommandPusher 支持结构化载荷的命令下发。
type FullCommandPusher interface {
	PushCommandFull(wsID string, typ aiev1.CommandType, employeeID, jobID, payloadJSON string, apply func(*aiev1.Command)) (*aiev1.Command, error)
}

// SetWorkflowMCP 注入工作流MCP。
func (s *Service) SetWorkflowMCP(wf *workflowmcp.Service, mcpPublicURL string) {
	s.WorkflowMCP = wf
	s.MCPPublicURL = mcpPublicURL
}

// SetMCPAuth 注入 MCP Token 服务。
func (s *Service) SetMCPAuth(a *mcpauth.Service) { s.MCPAuth = a }

// SetMCP 注入多用户 MCP 服务。
func (s *Service) SetMCP(m *mcp.Service) { s.MCP = m }

// SetFullPusher 注入支持结构化载荷的 Pusher。
func (s *Service) SetFullPusher(p FullCommandPusher) { s.FullPusher = p }

// SyncSkills 向工作站推送技能包同步命令。
func (s *Service) SyncSkills(ctx context.Context, workstationID, employeeID string, skillIDs []string) error {
	if s.WorkflowMCP == nil || s.FullPusher == nil {
		return fmt.Errorf("技能同步未就绪")
	}
	var packages []*aiev1.SkillPackage
	ids := skillIDs
	if len(ids) == 0 && employeeID != "" {
		scope, err := s.WorkflowMCP.ResolveScope(ctx, employeeID)
		if err != nil {
			return err
		}
		ids = scope.SkillIDs
	}
	for _, id := range ids {
		sp, md, err := s.WorkflowMCP.ExportSkill(ctx, id)
		if err != nil {
			continue
		}
		pkg := &aiev1.SkillPackage{
			Id: sp.ID, CursorName: sp.CursorName, Version: sp.Version,
			SkillMd: md, ContentHash: sp.ContentHash,
		}
		for _, f := range sp.Files {
			pkg.Files = append(pkg.Files, &aiev1.SkillFile{
				Path: f.Path, Content: f.Content, Sha256: f.SHA256,
			})
		}
		packages = append(packages, pkg)
	}
	payload, _ := json.Marshal(map[string]any{"employee_id": employeeID, "count": len(packages)})
	_, err := s.FullPusher.PushCommandFull(workstationID, aiev1.CommandType_COMMAND_TYPE_SYNC_SKILLS,
		employeeID, "", string(payload), func(cmd *aiev1.Command) {
			cmd.Structured = &aiev1.Command_SyncSkills{
				SyncSkills: &aiev1.SyncSkillsPayload{
					EmployeeId: employeeID,
					Packages:   packages,
				},
			}
		})
	return err
}

// buildStartJobPayload 组装 START_JOB 结构化载荷。
func (s *Service) buildStartJobPayload(ctx context.Context, employeeID, prompt, workspaceID, workspacePath, workflowID string) (*aiev1.StartJobPayload, error) {
	start := &aiev1.StartJobPayload{
		Prompt:        prompt,
		WorkspaceId:   workspaceID,
		WorkspacePath: workspacePath,
		Provider:      "cursor",
	}
	if s.Employees != nil {
		if e, err := s.Employees.Get(ctx, employeeID); err == nil && e != nil && e.DefaultProvider != "" {
			start.Provider = e.DefaultProvider
		}
	}
	if s.WorkflowMCP == nil {
		return start, nil
	}
	scope, err := s.WorkflowMCP.ResolveScope(ctx, employeeID)
	if err != nil {
		return start, nil
	}
	if workflowID != "" {
		if !scope.InScopeWorkflow(workflowID) {
			return nil, fmt.Errorf("员工未授权工作流 %s", workflowID)
		}
		wf, err := s.WorkflowMCP.GetWorkflow(ctx, workflowID)
		if err != nil {
			return nil, err
		}
		yamlOut, _ := workflowmcp.WorkflowToYAML(wf)
		start.WorkflowId = wf.ID
		start.WorkflowVersion = wf.Version
		start.WorkflowYaml = yamlOut
	}
	// 技能包
	for _, sid := range scope.SkillIDs {
		sp, md, err := s.WorkflowMCP.ExportSkill(ctx, sid)
		if err != nil {
			continue
		}
		start.SkillCursorNames = append(start.SkillCursorNames, sp.CursorName)
		pkg := &aiev1.SkillPackage{
			Id: sp.ID, CursorName: sp.CursorName, Version: sp.Version,
			SkillMd: md, ContentHash: sp.ContentHash,
		}
		for _, f := range sp.Files {
			pkg.Files = append(pkg.Files, &aiev1.SkillFile{
				Path: f.Path, Content: f.Content, Sha256: f.SHA256,
			})
		}
		start.SkillPackages = append(start.SkillPackages, pkg)
	}
	// 多用户 MCP 绑定下发：查询当前员工启用的所有 MCP 绑定 (§7, §30)
	hasWorkflowMCP := false
	if s.MCP != nil {
		if bnds, err := s.MCP.ListBindingsByEmployee(ctx, employeeID); err == nil && len(bnds) > 0 {
			for _, b := range bnds {
				if !b.Enabled {
					continue
				}
				if b.MCPServerID == "mcp-workflow" || strings.EqualFold(b.MCPServerName, "workflow-mcp") {
					hasWorkflowMCP = true
					continue
				}
				srv, err := s.MCP.GetServer(ctx, b.MCPServerID)
				if err != nil || srv.Status != mcp.StatusActive {
					continue
				}
				spec := &aiev1.MCPServerSpec{
					Name:    srv.Name,
					Type:    srv.Transport,
					Url:     srv.Endpoint,
					Headers: map[string]string{},
					Env:     map[string]string{},
				}
				if srv.Config != nil {
					if h, ok := srv.Config["headers"].(map[string]any); ok {
						for k, v := range h {
							if str, ok := v.(string); ok {
								spec.Headers[k] = str
							}
						}
					}
					if e, ok := srv.Config["env"].(map[string]any); ok {
						for k, v := range e {
							if str, ok := v.(string); ok {
								spec.Env[k] = str
							}
						}
					}
					if args, ok := srv.Config["args"].([]any); ok {
						for _, a := range args {
							if str, ok := a.(string); ok {
								spec.Args = append(spec.Args, str)
							}
						}
					}
					if cmd, ok := srv.Config["command"].(string); ok && cmd != "" {
						spec.Command = cmd
					}
				}
				if spec.Type == "" {
					spec.Type = "http"
				}
				if spec.Type == "stdio" && spec.Command == "" {
					spec.Command = srv.Endpoint
				}
				// 注入已绑定的身份凭证（若有）
				if b.CredentialID != "" {
					if secretVal, err := s.MCP.ResolveSecretValue(ctx, b.CredentialID); err == nil && secretVal != "" {
						spec.Headers["Authorization"] = "Bearer " + secretVal
					}
				}
				start.McpServers = append(start.McpServers, spec)
			}
		} else {
			// 若无任何绑定记录，保持向下兼容：默认启用 workflow-mcp
			hasWorkflowMCP = true
		}
	} else {
		hasWorkflowMCP = true
	}

	// 若绑定了 workflow-mcp（或兜底）：下发系统内置 workflow-mcp，携带员工动态 Token
	if hasWorkflowMCP && s.MCPPublicURL != "" && s.MCPAuth != nil {
		res, err := s.MCPAuth.Issue(ctx, mcpauth.SubjectEmployee, employeeID,
			mcpauth.ScopeRead, "job-runtime", "scheduler", 24*time.Hour)
		if err == nil && res != nil {
			start.McpServers = append(start.McpServers, &aiev1.MCPServerSpec{
				Name: "workflow-mcp",
				Type: "http",
				Url:  s.MCPPublicURL,
				Headers: map[string]string{
					"Authorization": "Bearer " + res.Secret,
				},
			})
		}
	}
	return start, nil
}

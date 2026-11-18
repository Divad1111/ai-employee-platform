package mcpauth

import "strings"

const (
	Workflow  = "workflow-mcp"
	Collector = "personal-work-collector"
)

type Builtin struct {
	ID, Name, Path, Description string
}

var Builtins = []Builtin{
	{"mcp-workflow", Workflow, "/mcp", "工作流、技能包与知识库"},
	{"mcp-collector", Collector, "/mcp/collector", "个人工作采集、飞书内容检索与操作"},
}

func FindBuiltin(id string) (Builtin, bool) {
	for _, b := range Builtins {
		if b.ID == id || b.Name == id {
			return b, true
		}
	}
	return Builtin{}, false
}

func Endpoint(workflowURL, service string) string {
	if service == Collector {
		return strings.TrimRight(workflowURL, "/") + "/collector"
	}
	return workflowURL
}

func (t *Token) AllowsService(service string) bool {
	audience := t.Service
	if audience == "" {
		audience = Workflow
	} // Old credentials never gain collector access.
	return audience == service
}

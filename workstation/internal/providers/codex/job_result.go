package codex

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/ai-employee-platform/workstation/internal/acp"
)

// Only the final message follows this schema. Commentary remains ordinary text.
var jobResultSchema = map[string]any{
	"type": "object", "additionalProperties": false,
	"required": []string{"status", "kind", "reply", "evidence"},
	"properties": map[string]any{
		"kind":     map[string]any{"type": "string", "enum": []string{"reply", "action"}, "description": "任务要求操作仓库、文件、外部服务或核实其状态时选 action；只需要交付本回复正文时选 reply。"},
		"status":   map[string]any{"type": "string", "enum": []string{"completed", "incomplete"}, "description": "只有用户要求的工作已经完成并核验后才能选 completed。遇阻、部分完成或仍待选择时选 incomplete。"},
		"reply":    map[string]any{"type": "string", "description": "给用户的完整最终答复，遵循用户要求的格式，说明实际结果。"},
		"evidence": map[string]any{"type": "string", "description": "操作类任务给出可核对的交付物、修订号或验证结果；问答类写明已回答的内容；未完成时说明原因。"},
	},
}

func (s *appServerSession) parseJobResult(raw string) (string, error) {
	var result struct{ Status, Kind, Reply, Evidence string }
	if json.Unmarshal([]byte(raw), &result) != nil || strings.TrimSpace(result.Reply) == "" {
		return "", errors.New("Codex 未提交有效的最终任务结果")
	}
	if result.Status != "completed" || strings.TrimSpace(result.Evidence) == "" {
		return result.Reply, errors.New("Codex 未确认完成任务：" + result.Evidence)
	}
	s.mu.Lock()
	toolCompleted := s.toolCompleted
	s.mu.Unlock()
	if result.Kind != "reply" && !(result.Kind == "action" && toolCompleted) {
		return result.Reply, errors.New("未取得支持该操作结果的成功工具记录")
	}
	s.emitActivity("完成核验：" + result.Evidence)
	return result.Reply, nil
}

func (s *appServerSession) emitActivity(text string) {
	if strings.TrimSpace(text) != "" {
		select {
		case s.events <- acp.Event{Type: "agent_activity", Payload: []byte(text)}:
		default:
		}
	}
}

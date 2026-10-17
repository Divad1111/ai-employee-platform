package taskconversation

import (
	"fmt"
	"html"
	"strings"

	"github.com/ai-employee-platform/server/internal/feishu"
)

// BuildCard 构建同一会话可反复刷新的飞书共享卡片。每个正文块都保持在飞书限制以内。
func BuildCard(c *Conversation, messages []Message, activities []Activity) *feishu.Card {
	statusText, color := statusDisplay(c.Status)
	title := "AI 员工任务对话"
	if c.CurrentJobID != "" {
		title += " · " + c.CurrentJobID
	}
	card := &feishu.Card{
		Config: feishu.CardConfig{WideScreenMode: true, EnableForward: true, UpdateMulti: true},
		Header: &feishu.CardHeader{Title: feishu.CardTitle{Tag: "plain_text", Content: title}, Template: color},
	}
	appendText := func(text string, max int) {
		card.Elements = append(card.Elements, feishu.CardElement{
			"tag":  "div",
			"text": map[string]interface{}{"tag": "lark_md", "content": truncateCardContent(text, max)},
		})
	}
	appendText("**状态：** "+statusText, 900)
	if c.EmployeeName != "" {
		appendText("**AI 员工：** "+truncateRunes(escapeCardText(c.EmployeeName), 820), 900)
	}
	if len(messages) > 4 {
		messages = messages[len(messages)-4:]
		appendText("_以下显示最近两轮；完整历史保存在服务端。_", 900)
	}
	if len(activities) > 4 {
		activities = activities[len(activities)-4:]
	}
	if len(activities) > 0 {
		appendText("**执行过程**", 900)
		for _, activity := range activities {
			appendText("• "+truncateRunes(escapeCardText(activity.Content), 820), 900)
		}
	} else if !isTerminal(c.Status) {
		appendText("**执行过程：** Agent 正在处理任务…", 900)
	}
	for _, m := range messages {
		label := "用户"
		if m.Role == "assistant" {
			label = "AI 员工"
		}
		text := truncateRunes(escapeCardText(m.Content), 820)
		if text == "" && m.Role == "assistant" && !isTerminal(m.Status) {
			text = "正在生成回复…"
		}
		appendText(fmt.Sprintf("**%s：**\n%s", label, text), 900)
	}

	if isTerminal(c.Status) {
		card.Elements = append(card.Elements, feishu.CardElement{
			"tag": "form", "name": "continue_form",
			"elements": []map[string]interface{}{
				{
					"tag": "input", "name": "prompt", "max_length": 1000,
					"placeholder":    map[string]interface{}{"tag": "plain_text", "content": "输入下一步要求，继续这段任务对话"},
					"label":          map[string]interface{}{"tag": "plain_text", "content": "继续对话"},
					"label_position": "top", "required": false,
				},
				{
					"tag": "button", "name": "continue", "action_type": "form_submit", "type": "primary",
					"text":  map[string]interface{}{"tag": "plain_text", "content": "发送并继续"},
					"value": map[string]interface{}{"action": "task_continue", "conversation_id": c.ID},
				},
			},
		})
	} else {
		card.Elements = append(card.Elements, feishu.CardElement{
			"tag": "action",
			"actions": []map[string]interface{}{{
				"tag": "button", "type": "danger",
				"text":  map[string]interface{}{"tag": "plain_text", "content": "停止任务"},
				"value": map[string]interface{}{"action": "task_stop", "conversation_id": c.ID},
			}},
		})
	}
	card.Elements = append(card.Elements, feishu.CardElement{
		"tag": "note", "elements": []map[string]interface{}{{"tag": "plain_text", "content": "一个任务会话对应一张卡片；后续轮次继续更新此卡片。"}},
	})
	return card
}

func statusDisplay(status string) (string, string) {
	switch strings.ToUpper(status) {
	case "CREATED", "QUEUED", "ASSIGNED", "STARTING":
		return "等待工作站执行", "blue"
	case "RUNNING":
		return "正在执行", "wathet"
	case "SUCCESS":
		return "已完成", "green"
	case "FAILED":
		return "执行失败", "red"
	case "CANCELLED":
		return "已停止", "grey"
	case "TIMEOUT":
		return "执行超时", "orange"
	case "WAITING_APPROVAL":
		return "等待确认", "orange"
	default:
		return status, "blue"
	}
}

func escapeCardText(text string) string {
	// 防止正文中的用户文本意外触发飞书 @ 标签。
	return html.EscapeString(text)
}

func truncateRunes(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "…（卡片仅展示部分内容）"
}

// truncateCardContent 限制 UTF-8 字节长度，确保中文内容也不会超过飞书卡片块的长度上限。
func truncateCardContent(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	const suffix = "…（已截断）"
	limit := maxBytes - len(suffix)
	var b strings.Builder
	for _, r := range text {
		piece := string(r)
		if b.Len()+len(piece) > limit {
			break
		}
		b.WriteString(piece)
	}
	return b.String() + suffix
}

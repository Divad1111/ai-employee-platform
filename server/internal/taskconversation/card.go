package taskconversation

import (
	"fmt"
	"html"
	"strings"
	"time"
	"unicode"

	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/job"
)

// BuildCard 构建与单个 Job 对应的飞书卡片。每个正文块都保持在飞书限制以内。
func BuildCard(c *Conversation, messages []Message, activities []Activity) *feishu.Card {
	return buildCardAt(c, messages, activities, time.Now())
}

func buildCardAt(c *Conversation, messages []Message, activities []Activity, now time.Time) *feishu.Card {
	jobMessages := make([]Message, 0, len(messages))
	for _, message := range messages {
		if message.JobID == c.CurrentJobID {
			jobMessages = append(jobMessages, message)
		}
	}
	messages = jobMessages
	statusText, color := statusDisplay(c.Status)
	schedulingBlocked := (c.Status == job.StatusCreated || c.Status == job.StatusQueued) && c.StatusReason != ""
	if schedulingBlocked {
		statusText, color = "调度受阻，等待重试", "orange"
	}
	title := "AI 员工任务"
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
	if !c.CreatedAt.IsZero() {
		appendText("**创建时间：** "+feishu.FormatCardTime(c.CreatedAt), 900)
	}
	if c.StatusReason != "" {
		label := "等待原因"
		if isTerminal(c.Status) {
			label = "失败原因"
		}
		for _, chunk := range splitEscapedCardText(c.StatusReason, 820) {
			appendText("**"+label+"：** "+chunk, 900)
		}
	}
	if !isTerminal(c.Status) && !c.CreatedAt.IsZero() {
		seconds := int64(now.Sub(c.CreatedAt).Seconds())
		if seconds < 0 {
			seconds = 0
		}
		appendText(fmt.Sprintf("**任务已接收：** %d 秒前。无需重复发送，相同员工的新任务会排队执行。", seconds), 900)
	}
	if c.EmployeeName != "" {
		appendText("**AI 员工：** "+truncateRunes(escapeCardText(c.EmployeeName), 820), 900)
	}
	if c.Provider != "" || c.Model != "" {
		provider, model := c.Provider, c.Model
		if provider == "" {
			provider = "未设置"
		}
		if model == "" {
			model = "Provider 默认模型"
		}
		appendText("**当前会话：** "+truncateRunes(escapeCardText(provider), 300)+"  ·  **模型：** "+truncateRunes(escapeCardText(model), 400), 900)
	} else {
		appendText("**当前会话：** 尚未启动", 900)
	}
	if c.DefaultProvider != "" || c.DefaultModel != "" {
		defaultProvider, defaultModel := c.DefaultProvider, c.DefaultModel
		if defaultProvider == "" {
			defaultProvider = "未设置"
		}
		if defaultModel == "" {
			defaultModel = "Provider 默认模型"
		}
		if defaultProvider != c.Provider || defaultModel != c.Model {
			appendText("**下次任务配置：** "+truncateRunes(escapeCardText(defaultProvider), 300)+"  ·  **模型：** "+truncateRunes(escapeCardText(defaultModel), 400), 900)
		}
	}
	if len(activities) > 10 {
		activities = activities[len(activities)-10:]
	}
	if len(activities) > 0 {
		appendText("**执行过程**", 900)
		for _, activity := range activities {
			appendText("• "+truncateRunes(escapeCardText(activity.Content), 820), 900)
		}
	} else if !isTerminal(c.Status) {
		progress := "Agent 正在处理任务…"
		switch c.Status {
		case job.StatusCreated, job.StatusQueued:
			progress = "任务排队中，尚未启动 Agent。"
			if schedulingBlocked {
				progress = "任务尚未启动；问题解决后会自动重试，无需重复发送。"
			}
		case job.StatusAssigned, job.StatusStarting:
			progress = "正在等待工作站启动 Agent。"
		case job.StatusBlocked:
			progress = "任务执行受阻，等待恢复。"
		case job.StatusWaitingApproval:
			progress = "等待确认后继续执行。"
		case job.StatusUnknown:
			progress = "工作站连接中断，正在确认任务状态。"
		}
		appendText("**执行过程：** "+progress, 900)
	}
	for _, m := range messages {
		label := "用户"
		if m.Role == "assistant" {
			label = "AI 员工"
		}
		text := m.Content
		if text == "" && m.Role == "assistant" {
			text = pendingReply(c.Status)
		}
		appendMessageText := func(content string) {
			chunks := splitEscapedCardText(content, 820)
			for i, chunk := range chunks {
				chunkLabel := label
				if i > 0 {
					chunkLabel += "（续）"
				}
				appendText(fmt.Sprintf("**%s：**\n%s", chunkLabel, chunk), 900)
			}
		}
		appendMessageText(text)
	}

	if !isTerminal(c.Status) {
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
		"tag": "note", "elements": []map[string]interface{}{{"tag": "plain_text", "content": "此卡片对应本次提问，执行过程和完整回复会更新在本卡片中；后续提问会创建新卡片。"}},
	})
	return card
}

// pendingReply 根据实际任务阶段提示，避免尚未启动或失败的任务仍显示正在生成回复。
func pendingReply(status string) string {
	switch status {
	case job.StatusCreated, job.StatusQueued:
		return "尚未开始执行，等待调度。"
	case job.StatusAssigned, job.StatusStarting:
		return "工作站正在启动任务，尚未生成回复。"
	case job.StatusSuccess:
		return "任务已完成，未返回文本回复。"
	case job.StatusFailed:
		return "任务执行失败，未返回回复；请查看失败原因。"
	case job.StatusCancelled:
		return "任务已停止。"
	case job.StatusTimeout:
		return "任务执行超时，未返回完整回复。"
	case job.StatusBlocked:
		return "任务执行受阻，等待恢复。"
	case job.StatusWaitingApproval:
		return "等待确认后继续执行。"
	case job.StatusUnknown:
		return "正在确认工作站上的任务状态。"
	default:
		return "正在生成回复…"
	}
}

func statusDisplay(status string) (string, string) {
	switch strings.ToUpper(status) {
	case "CREATED", "QUEUED":
		return "排队等待调度", "blue"
	case "ASSIGNED", "STARTING":
		return "等待工作站启动", "blue"
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
	case "BLOCKED":
		return "执行受阻", "orange"
	case "UNKNOWN":
		return "工作站连接中断，待确认", "orange"
	default:
		return status, "blue"
	}
}

func escapeCardText(text string) string {
	// 防止正文中的用户文本意外触发飞书 @ 标签。
	return html.EscapeString(text)
}

// splitEscapedCardText escapes and splits message text into card-sized chunks.
// Splitting by rune keeps HTML entities and UTF-8 characters intact, while
// preferring whitespace boundaries makes long replies easier to read.
func splitEscapedCardText(text string, maxBytes int) []string {
	if maxBytes < 1 {
		return []string{escapeCardText(text)}
	}
	type token struct {
		text       string
		whitespace bool
	}
	tokens := make([]token, 0, len([]rune(text)))
	for _, r := range text {
		tokens = append(tokens, token{text: html.EscapeString(string(r)), whitespace: unicode.IsSpace(r)})
	}
	if len(tokens) == 0 {
		return []string{""}
	}
	chunks := make([]string, 0, 1)
	for start := 0; start < len(tokens); {
		end, size, lastBoundary := start, 0, -1
		for end < len(tokens) && size+len(tokens[end].text) <= maxBytes {
			size += len(tokens[end].text)
			end++
			if tokens[end-1].whitespace {
				lastBoundary = end
			}
		}
		if end == len(tokens) {
			end = len(tokens)
		} else if lastBoundary > start && lastBoundary-start >= (end-start)*3/4 {
			end = lastBoundary
		}
		if end == start { // A single escaped rune is larger than maxBytes.
			end++
		}
		var b strings.Builder
		for _, part := range tokens[start:end] {
			b.WriteString(part.text)
		}
		chunks = append(chunks, b.String())
		start = end
	}
	return chunks
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

package feishu

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CardTemplate 飞书卡片顶栏主题色枚举
type CardTemplate string

const (
	CardTemplateBlue      CardTemplate = "blue"
	CardTemplateWathet    CardTemplate = "wathet"
	CardTemplateTurquoise CardTemplate = "turquoise"
	CardTemplateGreen     CardTemplate = "green"
	CardTemplateYellow    CardTemplate = "yellow"
	CardTemplateOrange    CardTemplate = "orange"
	CardTemplateRed       CardTemplate = "red"
	CardTemplateCarmine   CardTemplate = "carmine"
	CardTemplateViolet    CardTemplate = "violet"
	CardTemplatePurple    CardTemplate = "purple"
	CardTemplateIndigo    CardTemplate = "indigo"
	CardTemplateGrey      CardTemplate = "grey"
)

// Card 飞书消息卡片根对象 (支持 Markdown 风格内容)
type Card struct {
	Config   CardConfig    `json:"config"`
	Header   *CardHeader   `json:"header,omitempty"`
	Elements []CardElement `json:"elements"`
}

// CardConfig 卡片配置
type CardConfig struct {
	WideScreenMode bool `json:"wide_screen_mode"`
	EnableForward  bool `json:"enable_forward"`
}

// CardHeader 卡片头部
type CardHeader struct {
	Title    CardTitle `json:"title"`
	Template string    `json:"template,omitempty"`
}

// CardTitle 卡片标题
type CardTitle struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

// CardElement 卡片组件
type CardElement map[string]interface{}

// ToJSON 将卡片序列化为 JSON 字符串
func (c *Card) ToJSON() (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// MustJSON 序列化为 JSON，如果出错则返回空字符串
func (c *Card) MustJSON() string {
	s, _ := c.ToJSON()
	return s
}

// IsCardJSON 判断给定文本是否为飞书消息卡片的 JSON 字符串
func IsCardJSON(str string) bool {
	str = strings.TrimSpace(str)
	if !strings.HasPrefix(str, "{") || !strings.HasSuffix(str, "}") {
		return false
	}
	return strings.Contains(str, `"elements"`) || strings.Contains(str, `"schema"`)
}

// NewMarkdownCard 创建标准 Markdown 消息卡片
func NewMarkdownCard(title string, template CardTemplate, markdownContent string, footerText string) *Card {
	card := &Card{
		Config: CardConfig{
			WideScreenMode: true,
			EnableForward:  true,
		},
		Elements: make([]CardElement, 0, 4),
	}

	if title != "" {
		card.Header = &CardHeader{
			Title: CardTitle{
				Tag:     "plain_text",
				Content: title,
			},
			Template: string(template),
		}
	}

	if markdownContent != "" {
		card.Elements = append(card.Elements, CardElement{
			"tag": "div",
			"text": map[string]interface{}{
				"tag":     "lark_md",
				"content": markdownContent,
			},
		})
	}

	if footerText != "" {
		card.Elements = append(card.Elements, CardElement{
			"tag": "hr",
		})
		card.Elements = append(card.Elements, CardElement{
			"tag": "note",
			"elements": []map[string]interface{}{
				{
					"tag":     "plain_text",
					"content": footerText,
				},
			},
		})
	}

	return card
}

// BuildTaskCreatedCard 构建协同任务已创建的即时回执卡片
func BuildTaskCreatedCard(jobID, employeeName, instructionPreview string) *Card {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**任务编号**：`%s`\n", jobID))
	sb.WriteString(fmt.Sprintf("• 负责员工: %s\n", employeeName))
	sb.WriteString(fmt.Sprintf("• 任务指令: %s", instructionPreview))

	return NewMarkdownCard(
		"⏳ 协同任务已创建",
		CardTemplateBlue,
		sb.String(),
		"正在调度工作站执行，执行完毕后将自动向您同步结果...",
	)
}

// BuildJobResultCard 构建协同任务终态通知卡片（支持 Markdown 风格执行结果展示）
func BuildJobResultCard(jobID, status, summary string) *Card {
	title := "📋 协同任务执行通知"
	template := CardTemplateBlue
	statusText := status

	switch strings.ToUpper(status) {
	case "SUCCESS":
		title = "✅ 协同任务执行成功"
		template = CardTemplateGreen
		statusText = "已完成 (SUCCESS)"
	case "FAILED":
		title = "❌ 协同任务执行失败"
		template = CardTemplateRed
		statusText = "执行失败 (FAILED)"
	case "TIMEOUT":
		title = "⏰ 协同任务超时"
		template = CardTemplateOrange
		statusText = "已超时 (TIMEOUT)"
	case "CANCELLED":
		title = "⏹ 协同任务已取消"
		template = CardTemplateGrey
		statusText = "已取消 (CANCELLED)"
	}

	summary = strings.TrimSpace(summary)
	summary = strings.ReplaceAll(summary, "secret", "***")

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**任务编号**：`%s`\n", jobID))
	sb.WriteString(fmt.Sprintf("**执行状态**：`%s`\n", statusText))

	if summary != "" {
		sb.WriteString("\n---\n")
		if strings.ToUpper(status) == "SUCCESS" {
			sb.WriteString("### 📝 执行结果\n")
		} else {
			sb.WriteString("### ⚠️ 详情与日志\n")
		}
		sb.WriteString(summary)
	}

	nowStr := time.Now().Format("2006-01-02 15:04:05")
	return NewMarkdownCard(
		title,
		template,
		sb.String(),
		fmt.Sprintf("AI Employee 企业员工协同平台 • %s", nowStr),
	)
}

// BuildGuideCard 构建未识别数字员工时的使用指引卡片
func BuildGuideCard(rawText string) *Card {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("收到您的消息：%q\n\n", rawText))
	sb.WriteString("当前尚未识别到执行该任务的目标数字员工。\n\n")
	sb.WriteString("**💡 任务派发格式：**\n")
	sb.WriteString("• `@数字员工别名 任务需求`（例：`@AI员工 跑回归测试`）\n")
	sb.WriteString("• `/emp 别名 任务需求`（例：`/emp AI员工 跑回归测试`）\n")
	sb.WriteString("• `EMP-xxxx 任务需求`\n\n")
	sb.WriteString("> ℹ️ 提示：请在管理后台「数字员工」确认员工存在，并在「飞书企业协同」中绑定别名或当前单聊/群聊映射。")

	return NewMarkdownCard(
		"🤖 任务派发指引",
		CardTemplateOrange,
		sb.String(),
		"AI Employee 企业员工协同平台",
	)
}

// BuildNotAssignableCard 构建数字员工不可派单警告卡片
func BuildNotAssignableCard(employeeName string, err error) *Card {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("目标数字员工 **[%s]** 当前不可派单。\n\n", employeeName))
	sb.WriteString(fmt.Sprintf("**原因**：%v\n\n", err))
	sb.WriteString("> 💡 提示：请确认该员工处于启用状态并已绑定在线运行的工作站。")

	return NewMarkdownCard(
		"⚠️ 目标数字员工暂不可派单",
		CardTemplateOrange,
		sb.String(),
		"AI Employee 企业员工协同平台",
	)
}

// BuildTaskFailedCard 构建协同任务创建失败报警卡片
func BuildTaskFailedCard(err error) *Card {
	var sb strings.Builder
	sb.WriteString("协同任务创建遇到异常：\n\n")
	sb.WriteString(fmt.Sprintf("```\n%v\n```", err))

	return NewMarkdownCard(
		"❌ 协同任务创建失败",
		CardTemplateRed,
		sb.String(),
		"AI Employee 企业员工协同平台",
	)
}

// BuildWelcomeP2PCard 构建单聊建立欢迎卡片
func BuildWelcomeP2PCard(userName, openID, chatID string) *Card {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("你好 **%s**！欢迎使用 AI Employee 企业员工协同平台。\n\n", userName))
	sb.WriteString("**📌 您的专属标识凭证：**\n")
	sb.WriteString(fmt.Sprintf("• 用户 OpenID：`%s`\n", openID))
	sb.WriteString(fmt.Sprintf("• 单聊会话 Chat ID：`%s`\n\n", chatID))
	sb.WriteString("**💡 快速上手：**\n")
	sb.WriteString("1. 在此处直接回复指令派发任务；\n")
	sb.WriteString("2. 将机器人拉入项目群聊，并在群内 `@机器人` 协同；\n")
	sb.WriteString("3. 执行进展与终态回执将自动向您同步。")

	return NewMarkdownCard(
		"👋 欢迎使用 AI Employee 协同平台",
		CardTemplateBlue,
		sb.String(),
		"AI Employee 企业员工协同平台",
	)
}

// BuildWelcomeGroupCard 构建机器人进群欢迎卡片
func BuildWelcomeGroupCard(chatName, chatID string) *Card {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("AI Employee 协同助手已成功加入【**%s**】！\n\n", chatName))
	sb.WriteString(fmt.Sprintf("• 本群 Chat ID：`%s`\n\n", chatID))
	sb.WriteString("**💡 协同方式：**\n")
	sb.WriteString("在群聊中 `@机器人` 并输入工作需求（例如：`@AI员工 跑回归测试`），系统将自动调度工作站执行！")

	return NewMarkdownCard(
		"🎉 协同助手已加入群聊",
		CardTemplateBlue,
		sb.String(),
		"AI Employee 企业员工协同平台",
	)
}

// BuildTestMessageCard 构建测试通信消息卡片
func BuildTestMessageCard(content string) *Card {
	if content == "" {
		content = "【AI Employee 协同平台】通信测试正常"
	}
	nowStr := time.Now().Format("2006-01-02 15:04:05")
	var sb strings.Builder
	sb.WriteString(content)
	sb.WriteString("\n\n---\n")
	sb.WriteString(fmt.Sprintf("• 发送时间：`%s`\n", nowStr))
	sb.WriteString("• 传输通道：飞书官方 Go SDK (WebSocket / Webhook)\n")
	sb.WriteString("• 消息格式：交互式 Markdown 消息卡片")

	return NewMarkdownCard(
		"📡 AI Employee 通信测试",
		CardTemplateBlue,
		sb.String(),
		"AI Employee 企业员工协同平台",
	)
}

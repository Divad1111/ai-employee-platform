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
func BuildTaskCreatedCard(jobID, employeeName, instructionPreview, identityNote string) *Card {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**任务编号**：`%s`\n", jobID))
	sb.WriteString(fmt.Sprintf("• 负责员工: %s\n", employeeName))
	sb.WriteString(fmt.Sprintf("• 任务指令: %s", instructionPreview))
	if identityNote != "" {
		sb.WriteString("\n\n")
		sb.WriteString(identityNote)
	}

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

// BuildGuideCard 构建未识别数字员工时的使用指引卡片（兼容旧调用）。
func BuildGuideCard(rawText string) *Card {
	_ = rawText
	return BuildMentionRequiredCard(nil)
}

// BuildMentionRequiredCard 私聊未 @ 员工时的提示卡片：请 @ 对应员工执行。
func BuildMentionRequiredCard(aliases []string) *Card {
	var sb strings.Builder
	sb.WriteString("请 **@对应员工** 后再发送任务，否则无法派单执行。\n\n")
	sb.WriteString("**派发格式示例：**\n")
	if len(aliases) > 0 {
		sb.WriteString(fmt.Sprintf("• `@%s 任务需求`\n", aliases[0]))
		sb.WriteString(fmt.Sprintf("• `/emp %s 任务需求`\n", aliases[0]))
	} else {
		sb.WriteString("• `@数字员工别名 任务需求`\n")
		sb.WriteString("• `/emp 别名 任务需求`\n")
	}
	sb.WriteString("• `EMP-xxxx 任务需求`\n\n")
	if len(aliases) > 0 {
		sb.WriteString("**当前可 @ 的员工别名：** ")
		for i, a := range aliases {
			if i > 0 {
				sb.WriteString("、")
			}
			sb.WriteString("`@" + a + "`")
		}
		sb.WriteString("\n\n")
	}
	sb.WriteString("> ℹ️ 群聊中未 @ 数字员工的消息会被忽略；私聊也需要明确 @ 目标员工。")

	return NewMarkdownCard(
		"请@对应员工执行",
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
func BuildTaskFailedCard(err error, identityNote string) *Card {
	var sb strings.Builder
	sb.WriteString("协同任务创建遇到异常：\n\n")
	sb.WriteString(fmt.Sprintf("```\n%v\n```", err))
	if identityNote != "" {
		sb.WriteString("\n\n")
		sb.WriteString(identityNote)
	}

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

// InquiryCardOption 选项配置
type InquiryCardOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
}

// BuildInquiryCard 构建带交互按钮的决策/安全选项确认卡片
func BuildInquiryCard(jobID, employeeName, inquiryID, message string, options []InquiryCardOption) *Card {
	card := &Card{
		Config: CardConfig{
			WideScreenMode: true,
			EnableForward:  true,
		},
		Header: &CardHeader{
			Title: CardTitle{
				Tag:     "plain_text",
				Content: "🔔 AI 员工决策/安全请示",
			},
			Template: string(CardTemplateOrange),
		},
		Elements: make([]CardElement, 0, 5),
	}

	var sb strings.Builder
	if employeeName != "" {
		sb.WriteString(fmt.Sprintf("**负责员工**：%s\n", employeeName))
	}
	sb.WriteString(fmt.Sprintf("**任务编号**：`%s`\n", jobID))
	sb.WriteString(fmt.Sprintf("**请示编号**：`%s`\n\n", inquiryID))
	sb.WriteString("**请示说明：**\n")
	sb.WriteString(fmt.Sprintf("> %s\n\n", message))
	sb.WriteString("**候选选项列表：**\n")
	for i, opt := range options {
		sb.WriteString(fmt.Sprintf("• **[%d]** %s (`%s`)\n", i+1, opt.Name, opt.OptionID))
	}
	sb.WriteString("\n> 💡 **交互说明**：您可以直接点击下方按钮快速选择；也可以在当前会话中直接回复选项数字序号（如 `1`）或选项名称。")

	card.Elements = append(card.Elements, CardElement{
		"tag": "div",
		"text": map[string]interface{}{
			"tag":     "lark_md",
			"content": sb.String(),
		},
	})

	// 按钮操作组件
	actions := make([]map[string]interface{}, 0, len(options))
	for _, opt := range options {
		btnType := "default"
		lowerID := strings.ToLower(opt.OptionID)
		if strings.Contains(lowerID, "allow") || strings.Contains(opt.Name, "允许") || strings.Contains(opt.Name, "通过") {
			btnType = "primary"
		} else if strings.Contains(lowerID, "deny") || strings.Contains(lowerID, "reject") || strings.Contains(opt.Name, "拒绝") || strings.Contains(opt.Name, "阻断") {
			btnType = "danger"
		}
		actions = append(actions, map[string]interface{}{
			"tag": "button",
			"text": map[string]interface{}{
				"tag":     "plain_text",
				"content": opt.Name,
			},
			"type": btnType,
			"value": map[string]interface{}{
				"action":      "resolve_inquiry",
				"inquiry_id":  inquiryID,
				"option_id":   opt.OptionID,
				"option_name": opt.Name,
				"job_id":      jobID,
			},
		})
	}

	card.Elements = append(card.Elements, CardElement{
		"tag": "hr",
	})
	card.Elements = append(card.Elements, CardElement{
		"tag":     "action",
		"actions": actions,
	})

	card.Elements = append(card.Elements, CardElement{
		"tag": "note",
		"elements": []map[string]interface{}{
			{
				"tag":     "plain_text",
				"content": fmt.Sprintf("AI Employee 交互网关 • 请示于 %s", time.Now().Format("15:04:05")),
			},
		},
	})

	return card
}

// BuildInquiryResolvedCard 构建已选择确认的回执卡片
func BuildInquiryResolvedCard(jobID, employeeName, inquiryID, chosenName, chosenBy, via string) *Card {
	var sb strings.Builder
	if employeeName != "" {
		sb.WriteString(fmt.Sprintf("**负责员工**：%s\n", employeeName))
	}
	sb.WriteString(fmt.Sprintf("**任务编号**：`%s`\n", jobID))
	sb.WriteString(fmt.Sprintf("**请示编号**：`%s`\n", inquiryID))
	sb.WriteString(fmt.Sprintf("**选择结果**：【**%s**】\n", chosenName))
	if chosenBy != "" {
		sb.WriteString(fmt.Sprintf("**操作人员**：`%s`\n", chosenBy))
	}
	sb.WriteString(fmt.Sprintf("**交互方式**：%s\n\n", via))
	sb.WriteString("✅ 选项已成功回传宿主工作站，AI Agent 正在继续执行中...")

	return NewMarkdownCard(
		"✅ 决策/权限请示已处理",
		CardTemplateGreen,
		sb.String(),
		fmt.Sprintf("AI Employee 交互网关 • 完成于 %s", time.Now().Format("15:04:05")),
	)
}

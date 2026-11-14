package feishu

import (
	"html"
	"time"
)

const InquiryTextSubmitPrefix = "inquiry_text_"
const InquiryAnswerField = "answer"

func buildInputInquiryCard(jobID, employeeName, inquiryID, message string, options []InquiryCardOption) *Card {
	if employeeName == "" {
		employeeName = "AI 员工"
	}
	card := &Card{
		Schema: "2.0",
		Config: CardConfig{EnableForward: false, UpdateMulti: true, WidthMode: "default"},
		Header: &CardHeader{Title: CardTitle{Tag: "plain_text", Content: employeeName + " · 等待回答"}, Template: "blue"},
		Body:   &CardBody{},
	}
	card.Body.Elements = append(card.Body.Elements, CardElement{"tag": "markdown", "content": "**员工：** " + html.EscapeString(employeeName) + "\n**状态：** 等待回答\n**请求时间：** " + FormatCardTime(time.Now()) + "\n\n" + html.EscapeString(message)})
	for _, option := range options {
		card.Body.Elements = append(card.Body.Elements, CardElement{
			"tag": "button", "type": "default", "width": "fill",
			"text": map[string]any{"tag": "plain_text", "content": option.Name},
			"behaviors": []map[string]any{{"type": "callback", "value": map[string]any{
				"action": "resolve_inquiry", "inquiry_id": inquiryID, "option_id": option.OptionID, "job_id": jobID,
			}}},
		})
	}
	label := "你的回答"
	if len(options) > 0 {
		label = "也可以输入其他回答"
	}
	card.Body.Elements = append(card.Body.Elements, CardElement{
		"tag": "form", "name": "answer_form",
		"elements": []CardElement{
			{"tag": "input", "name": InquiryAnswerField, "required": true, "input_type": "multiline_text", "max_length": 1000, "width": "fill",
				"label":       map[string]any{"tag": "plain_text", "content": label},
				"placeholder": map[string]any{"tag": "plain_text", "content": "在这里输入内容，再点击提交回答"}},
			{"tag": "button", "name": InquiryTextSubmitPrefix + inquiryID, "form_action_type": "submit", "type": "primary_filled", "width": "fill",
				"text": map[string]any{"tag": "plain_text", "content": "提交回答"}},
		},
	})
	return card
}

func BuildInquirySubmittedCard(jobID, employeeName, answer string, submittedAt time.Time, allowText bool) *Card {
	if employeeName == "" {
		employeeName = "AI 员工"
	}
	title := employeeName + " · 已提交"
	text := "**员工：** " + html.EscapeString(employeeName) + "\n**状态：** 已提交\n**提交时间：** " + FormatCardTime(submittedAt) + "\n\n**你的回答**\n" + html.EscapeString(answer)
	if !allowText {
		return NewMarkdownCard(title, CardTemplateGreen, text, "")
	}
	return &Card{
		Schema: "2.0", Config: CardConfig{UpdateMulti: true, WidthMode: "default"},
		Header: &CardHeader{Title: CardTitle{Tag: "plain_text", Content: title}, Template: "green"},
		Body:   &CardBody{Elements: []CardElement{{"tag": "markdown", "content": text}}},
	}
}

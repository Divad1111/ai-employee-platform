package acp

import (
	"fmt"
	"strings"
)

// Text-only providers lack a final response schema. Put the outcome outside
// the user's deliverable so JSON, code and other requested formats survive.
const ResultInstructions = "执行并核验本轮要求后，在最终答复正文之后另起一行写【任务结果】已完成：具体核验依据。未完成则写【任务结果】未完成：实际阻碍。问答类以已交付的答复为依据。"

func ReadResult(text string) (reply, evidence string, err error) {
	text = strings.TrimSpace(text)
	i := strings.LastIndex(text, "\n【任务结果】")
	if i < 0 {
		return text, "", fmt.Errorf("Agent 未提交任务完成依据")
	}
	reply = strings.TrimSpace(text[:i])
	status := strings.TrimSpace(text[i+len("\n【任务结果】"):])
	evidence, ok := strings.CutPrefix(status, "已完成：")
	if !ok || reply == "" || strings.TrimSpace(evidence) == "" {
		return reply, "", fmt.Errorf("Agent 未确认完成任务：%s", status)
	}
	return reply, evidence, nil
}

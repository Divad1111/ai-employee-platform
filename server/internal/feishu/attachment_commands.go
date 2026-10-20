package feishu

import (
	"context"
	"fmt"
	"strings"
)

func commandPendingFiles(s *Service, ctx context.Context, ev IncomingEvent, _ string, args []string) string {
	if len(args) != 0 {
		return "用法: `/files`"
	}
	ev.Attachments = nil
	files := s.taskAttachments(ev)
	var b strings.Builder
	if len(files) == 0 {
		b.WriteString("当前会话没有待派单附件。可以先发图片/文件, 再发送 @员工 任务需求。\n")
	} else {
		fmt.Fprintf(&b, "**待派单附件 (%d):**\n", len(files))
		for i, file := range files {
			fmt.Fprintf(&b, "%d. %s\n", i+1, file.Name)
		}
		b.WriteString("发送 @员工 任务需求 即可使用; /clear files 清空。暂存 15 分钟, 服务重启后需重发或回复原附件消息。\n")
	}
	s.mu.RLock()
	handler := s.TaskConversations
	s.mu.RUnlock()
	if history, ok := handler.(AttachmentHistory); ok {
		jobs, err := history.RecentAttachmentJobs(ctx, ev.ChatID, ev.SenderOpenID, 5)
		if err != nil {
			b.WriteString("\n最近任务附件暂时无法查询, 请稍后重试。\n")
		} else if len(jobs) == 0 {
			b.WriteString("\n自己在当前会话最近 100 条任务中没有附件记录。\n")
		} else {
			b.WriteString("\n**最近任务已使用的附件 (自己在当前会话, 最多 5 条任务):**\n")
			for _, j := range jobs {
				fmt.Fprintf(&b, "\n`%s` · %s\n", j.JobID, j.EmployeeName)
				for _, file := range j.Files {
					fmt.Fprintf(&b, "• %s (%d 字节)\n", file.Name, file.Size)
				}
			}
		}
	}
	b.WriteString("\n附件成功派单后会从暂存区移到任务记录, 不会自动用于新任务。再次使用请回复原附件消息并 @员工 派单。/clear files 仅清空待派单附件。")
	return b.String()
}

func commandClearFiles(s *Service, _ context.Context, ev IncomingEvent, _ string, args []string) string {
	if len(args) != 0 {
		return "用法: `/clear files`"
	}
	s.mu.Lock()
	delete(s.pendingFiles, s.attachmentKey(ev))
	s.mu.Unlock()
	return "已清空自己在当前会话的待派单附件。"
}

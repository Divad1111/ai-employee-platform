package taskconversation

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ai-employee-platform/server/internal/job"
)

func TestSchedulingBlockAndFailureReachExistingCard(t *testing.T) {
	ctx := context.Background()
	var contents []string
	var creates int
	s := cardTestService(t, func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			return cardResponse(`{"code":0,"tenant_access_token":"test-token","expire":7200}`), nil
		}
		body, _ := io.ReadAll(r.Body)
		contents = append(contents, string(body))
		if r.Method == http.MethodPatch {
			return cardResponse(`{"code":0}`), nil
		}
		creates++
		return cardResponse(`{"code":0,"data":{"message_id":"om-state","chat_id":"oc-chat"}}`), nil
	})
	c := createCardTask(t, s, "TC-state", "oc-chat")
	if err := s.Jobs.ReportSchedulingBlock(ctx, c.CurrentJobID, "工作站角色授权已撤销"); err != nil {
		t.Fatal(err)
	}
	if err := s.refreshCard(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	blocked := contents[len(contents)-1]
	if !strings.Contains(blocked, "调度受阻，等待重试") || !strings.Contains(blocked, "工作站角色授权已撤销") || strings.Contains(blocked, "Agent 正在处理任务") || strings.Contains(blocked, "正在生成回复") {
		t.Fatal("收单卡片必须展示已有阻塞原因，不能声称 Agent 已启动", blocked)
	}
	// 状态说明不能写入回复历史，否则后续任务会把调度错误当作 Agent 回复。
	messages, _ := s.Store.Messages(ctx, c.ID)
	if messages[1].Content != "" {
		t.Fatal("调度错误污染了会话上下文")
	}
	for _, status := range []string{job.StatusQueued, job.StatusAssigned, job.StatusStarting, job.StatusRunning} {
		if _, err := s.Jobs.Transition(ctx, c.CurrentJobID, status, "scheduler", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.refreshCard(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	running := contents[len(contents)-1]
	if strings.Contains(running, "工作站角色授权已撤销") || !strings.Contains(running, "正在执行") {
		t.Fatal("恢复执行后应清除过期阻塞原因", running)
	}
	s.OnAgentDelta(ctx, "partial-reply", c.CurrentJobID, 1, "已完成部分检查")
	if _, err := s.Jobs.Transition(ctx, c.CurrentJobID, job.StatusFailed, "workstation", "", map[string]string{"error": "Provider 启动失败"}); err != nil {
		t.Fatal(err)
	}
	// 即使终态回调尚未更新会话，也应从持久化任务中恢复结果和失败原因。
	if err := s.refreshCard(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	failed := contents[len(contents)-1]
	if !strings.Contains(failed, "执行失败") || !strings.Contains(failed, "Provider 启动失败") || !strings.Contains(failed, "已完成部分检查") || strings.Contains(failed, "正在生成回复") {
		t.Fatal("失败必须更新同一张卡片，保留已有回复并说明错误", failed)
	}
	if creates != 1 {
		t.Fatalf("任务状态更新不应新建重复卡片: %d", creates)
	}
}

func TestCardPlaceholdersFollowActualJobState(t *testing.T) {
	for _, tt := range []struct{ status, want string }{
		{job.StatusCreated, "尚未开始执行，等待调度"},
		{job.StatusQueued, "尚未开始执行，等待调度"},
		{job.StatusStarting, "工作站正在启动任务"},
		{job.StatusRunning, "正在生成回复"},
		{job.StatusFailed, "任务执行失败"},
		{job.StatusTimeout, "任务执行超时"},
		{job.StatusCancelled, "任务已停止"},
		{job.StatusSuccess, "任务已完成"},
		{job.StatusWaitingApproval, "等待确认后继续执行"},
		{job.StatusUnknown, "正在确认工作站上的任务状态"},
	} {
		t.Run(tt.status, func(t *testing.T) {
			c := &Conversation{CurrentJobID: "JOB", Status: tt.status}
			text := BuildCard(c, []Message{{JobID: "JOB", Role: "assistant", Status: job.StatusRunning}}, nil).MustJSON()
			if !strings.Contains(text, tt.want) {
				t.Fatal("卡片未展示实际任务阶段", text)
			}
			if tt.status != job.StatusRunning && (strings.Contains(text, "正在生成回复") || strings.Contains(text, "Agent 正在处理任务")) {
				t.Fatal("非执行状态仍显示 Agent 正在处理", text)
			}
		})
	}
}

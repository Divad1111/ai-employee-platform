package feishu_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/secret"
	"github.com/ai-employee-platform/server/internal/taskconversation"
	"github.com/ai-employee-platform/server/internal/workstation"
)

func commandFixture(t *testing.T) (*feishu.Service, *feishu.MemorySender, *employee.Service, string, *fakeJobs) {
	t.Helper()
	ctx := context.Background()
	vault, _ := secret.NewMemoryVault()
	fs := feishu.NewService(vault)
	sender, jobs := &feishu.MemorySender{}, &fakeJobs{}
	fs.Sender, fs.Jobs = sender, jobs
	employees := employee.NewService(employee.NewMemoryStore(), audit.NewMemory(), eventbus.New(10))
	e, err := employees.Create(ctx, employee.CreateInput{Name: "测试员工", OwnerUserID: "user", WorkstationID: "WS-1", DefaultProvider: "codex", DefaultModel: "model-old"}, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	ca, err := certca.NewDevAuthority()
	if err != nil {
		t.Fatal(err)
	}
	presence := reliability.NewPresence(5, 15)
	presence.Touch("WS-1", "v1", 1, 1, 0, 0, 0)
	presence.SetProviders("WS-1", []string{"codex"})
	presence.SetModels("WS-1", map[string][]reliability.ModelOption{"codex": {{ID: "model-new", Label: "New Model"}, {ID: "model-old"}}})
	ws := workstation.NewService(ca, presence, nil)
	bridge := &feishu.Bridge{Employees: employees, Feishu: fs, Workstations: ws}
	fs.Commands, fs.Employees = bridge, bridge
	fs.UpsertBinding(feishu.Binding{EmployeeID: e.ID, FeishuAlias: "test3", FeishuOpenID: "owner"})
	conversations := taskconversation.NewService(taskconversation.NewMemoryStore(), fs, nil, nil)
	conversations.Employees, conversations.Workstations = employees, ws
	fs.SetTaskConversationHandler(conversations)
	return fs, sender, employees, e.ID, jobs
}

func TestChatQueriesAndInvalidCommandsNeverCreateJobs(t *testing.T) {
	fs, sender, _, id, jobs := commandFixture(t)
	for i, tc := range []struct{ text, want string }{
		{"/help", "/change model"},
		{"@机器人 /help", "/whoami"},
		{"/HELP", "/list emp"},
		{"/list emp", "可派单"},
		{"/list emp extra", "用法"},
		{"/model", "model-new"},
		{"/models test3", "model-old"},
		{"@test3 /model", "New Model"},
		{"/emp test3 /model", "model-new"},
		{"/employee test3 /status", "model-old"},
		{"/status @test3", "ONLINE"},
		{"/status " + id, "测试员工"},
		{"/status " + strings.ToUpper(id), "测试员工"},
		{"@test3 /status", "model-old"},
		{"/whoami", "test3"},
		{"/helpful", "未识别的命令"},
		{"@test3 /models-typo", "未识别的命令"},
		{"/change mode", "请指定员工"},
		{"/model nobody", "找不到该员工"},
	} {
		t.Run(tc.text, func(t *testing.T) {
			job, dup, err := fs.HandleMessage(context.Background(), feishu.IncomingEvent{EventID: fmt.Sprint(i), ChatID: "chat", ChatType: "group", SenderOpenID: "owner", Text: tc.text})
			if err != nil || dup || job != "" {
				t.Fatalf("job=%q duplicate=%v err=%v", job, dup, err)
			}
			if len(sender.Sent) != i+1 {
				t.Fatalf("expected %d replies, got %d", i+1, len(sender.Sent))
			}
			if !strings.Contains(sender.Sent[i].Content, tc.want) {
				t.Fatalf("expected %q, got %s", tc.want, sender.Sent[i].Content)
			}
			if !json.Valid([]byte(sender.Sent[i].Content)) {
				t.Fatal("invalid card JSON")
			}
		})
	}
	if len(jobs.created) != 0 {
		t.Fatal("query or malformed command created a task")
	}
}

func TestEmployeeControlAuthorizationAndPersistence(t *testing.T) {
	for _, text := range []string{"/change model test3 codex model-new", "/change mode @test3 New Model", "@test3 /change model model-new", "@test3 /model model-new", "@test3 切换模型 model-new", "/new test3", "@test3 新建会话"} {
		t.Run(text, func(t *testing.T) {
			fs, sender, employees, id, jobs := commandFixture(t)
			ctx := context.Background()
			ev := feishu.IncomingEvent{EventID: "unauthorized", ChatID: "chat", ChatType: "p2p", SenderOpenID: "stranger", Text: text}
			if _, _, err := fs.HandleMessage(ctx, ev); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(sender.Sent[0].Content, "未绑定") || fs.EmployeeHasOpenID(id, "stranger") {
				t.Fatal("control implicitly granted permission")
			}
			e, _ := employees.Get(ctx, id)
			if e.DefaultModel != "model-old" || e.SessionGeneration != 0 {
				t.Fatal("unauthorized control changed configuration")
			}
			ev.EventID, ev.SenderOpenID = "authorized", "owner"
			if _, _, err := fs.HandleMessage(ctx, ev); err != nil {
				t.Fatal(err)
			}
			e, _ = employees.Get(ctx, id)
			if strings.HasPrefix(text, "/new ") || strings.Contains(text, "新建会话") {
				if e.SessionGeneration != 1 {
					t.Fatal("new session was not persisted")
				}
			} else if e.DefaultModel != "model-new" {
				t.Fatal("model switch was not persisted")
			}
			if len(jobs.created) != 0 {
				t.Fatal("control created a task")
			}
			_, dup, _ := fs.HandleMessage(ctx, ev)
			if !dup || len(sender.Sent) != 2 {
				t.Fatal("duplicate command was executed")
			}
		})
	}
}

func TestInvalidModelDoesNotChangeConfiguration(t *testing.T) {
	fs, sender, employees, id, _ := commandFixture(t)
	_, _, err := fs.HandleMessage(context.Background(), feishu.IncomingEvent{Text: "/change model test3 missing-model", SenderOpenID: "owner", ChatID: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	e, _ := employees.Get(context.Background(), id)
	if e.DefaultModel != "model-old" || !strings.Contains(sender.Sent[0].Content, "没有可用模型") {
		t.Fatal("invalid model accepted")
	}
}

func TestUnsupportedFormatsAndFallbackShowCommands(t *testing.T) {
	for _, tc := range []struct {
		text, kind, chat string
		reply            bool
	}{
		{"hello", "text", "p2p", true},
		{"hello", "text", "group", false},
		{"/unknown", "text", "group", true},
		{"@test3", "image", "group", true},
		{"@test3", "text", "p2p", true},
		{"{\"file_key\":\"file\"}", "file", "p2p", true},
		{"{\"image_key\":\"image\"}", "image", "group", false},
	} {
		fs, sender, _, _, jobs := commandFixture(t)
		_, _, _ = fs.HandleMessage(context.Background(), feishu.IncomingEvent{Text: tc.text, MessageType: tc.kind, ChatType: tc.chat, ChatID: "chat"})
		if (len(sender.Sent) > 0) != tc.reply {
			t.Fatalf("unexpected reply for %+v", tc)
		}
		if tc.reply && !strings.Contains(sender.Sent[0].Content, "/help") {
			t.Fatal("missing command help")
		}
		if len(jobs.created) != 0 {
			t.Fatal("unsupported input became a task")
		}
	}
}

func TestWebhookPreservesImageAttachmentAndMentions(t *testing.T) {
	fs, sender, _, _, jobs := commandFixture(t)
	_, _, ev, err := feishu.ParseWebhookBody([]byte(`{"header":{"event_type":"im.message.receive_v1","event_id":"image-event"},"event":{"message":{"message_id":"image-message","chat_id":"chat","chat_type":"group","message_type":"image","content":"{\"image_key\":\"image\"}","mentions":[{"key":"@_user_1"}]}}}`))
	if err != nil || ev == nil || ev.MessageType != "image" || !ev.HasMentions {
		t.Fatalf("unsupported type or mention lost: %+v, %v", ev, err)
	}
	ev.SenderOpenID = "owner"
	if _, _, err = fs.HandleMessage(context.Background(), *ev); err != nil {
		t.Fatal(err)
	}
	if len(jobs.created) != 0 || len(sender.Sent) != 1 || !strings.Contains(sender.Sent[0].Content, "已收到附件") || len(ev.Attachments) != 1 {
		t.Fatal("image was not staged with guidance")
	}
}

func TestGeneratedEmployeeIDTaskDispatch(t *testing.T) {
	fs, _, _, id, jobs := commandFixture(t)
	for _, text := range []string{id + " 请执行任务", "/emp " + id + " 请执行任务", "@test3 请执行任务"} {
		jobID, _, err := fs.HandleMessage(context.Background(), feishu.IncomingEvent{Text: text, ChatID: "chat", SenderOpenID: "owner"})
		if err != nil || jobID == "" {
			t.Fatalf("ordinary dispatch failed: %q: %v", text, err)
		}
		if !strings.HasPrefix(jobs.created[len(jobs.created)-1], id+"|") {
			t.Fatal("employee ID case was changed")
		}
	}
}

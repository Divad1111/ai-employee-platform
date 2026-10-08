package feishu_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/message"
	"github.com/ai-employee-platform/server/internal/notification"
	"github.com/ai-employee-platform/server/internal/secret"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type fakeJobs struct {
	created []string
	prompts []string
}

func (f *fakeJobs) CreateFromFeishu(_ context.Context, employeeID, prompt, idem, chat, msg, sender string) (string, error) {
	f.created = append(f.created, employeeID+"|"+idem+"|"+sender)
	f.prompts = append(f.prompts, prompt)
	return "JOB-1", nil
}

type fakeEmp struct{}

func (fakeEmp) ResolveAlias(context.Context, string) (string, error) { return "", feishu.ErrNoEmployee }
func (fakeEmp) IsAssignable(context.Context, string) error           { return nil }
func (fakeEmp) GetEmployeeName(context.Context, string) string       { return "AI员工" }

func TestParseTargetAliasAndEmpID(t *testing.T) {
	v, _ := secret.NewMemoryVault()
	s := feishu.NewService(v)
	s.UpsertBinding(feishu.Binding{EmployeeID: "EMP-A", FeishuAlias: "alice"})
	id, prompt, err := s.ParseTarget("@alice please fix nil")
	if err != nil || id != "EMP-A" || prompt == "" {
		t.Fatalf("%s %q %v", id, prompt, err)
	}
	id, prompt, err = s.ParseTarget("EMP-XYZ do work")
	if err != nil || id != "EMP-XYZ" {
		t.Fatal(id, err)
	}
	id, prompt, err = s.ParseTarget("/emp alice ship it")
	if err != nil || id != "EMP-A" || prompt != "ship it" {
		t.Fatal(id, prompt, err)
	}
	s.UpsertBinding(feishu.Binding{EmployeeID: "EMP-K", FeishuAlias: "可乐1"})
	id, prompt, err = s.ParseTarget("@AI员工 /emp 可乐1 你是谁，用的什么模型")
	if err != nil || id != "EMP-K" || prompt != "你是谁，用的什么模型" {
		t.Fatalf("群里先 @机器人再 /emp 应能派单: id=%s prompt=%q err=%v", id, prompt, err)
	}
}

func TestVerifySignatureAndDedupe(t *testing.T) {
	v, _ := secret.NewMemoryVault()
	s := feishu.NewService(v)
	s.SetConfig(feishu.Config{VerificationToken: "tok"})
	body := `{"x":1}`
	ts, nonce := "1", "n"
	sum := sha256.Sum256([]byte(ts + nonce + "tok" + body))
	sig := hex.EncodeToString(sum[:])
	if err := s.VerifySignature(ts, nonce, sig, body); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifySignature(ts, nonce, "bad", body); err != feishu.ErrBadSignature {
		t.Fatal(err)
	}
	if s.Dedupe("e1") {
		t.Fatal("first")
	}
	if !s.Dedupe("e1") {
		t.Fatal("dup")
	}
}

func TestHandleMessageIdempotentAndNotify(t *testing.T) {
	v, _ := secret.NewMemoryVault()
	s := feishu.NewService(v)
	s.UpsertBinding(feishu.Binding{EmployeeID: "EMP-1", FeishuAlias: "bot"})
	fj := &fakeJobs{}
	s.Jobs = fj
	s.Employees = fakeEmp{}
	sender := &feishu.MemorySender{}
	s.Sender = sender

	ev := feishu.IncomingEvent{EventID: "ev1", MessageID: "m1", ChatID: "c1", SenderOpenID: "ou_1", Text: "@bot hello"}
	id, dup, err := s.HandleMessage(context.Background(), ev)
	if err != nil || dup || id != "JOB-1" {
		t.Fatal(id, dup, err)
	}
	_, dup, err = s.HandleMessage(context.Background(), ev)
	if err != nil || !dup {
		t.Fatal(dup, err)
	}
	if err := s.NotifyJobResult(context.Background(), "c1", "JOB-1", "SUCCESS", "done"); err != nil {
		t.Fatal(err)
	}
	if len(sender.Sent) < 2 {
		t.Fatal("缺少即时确认或终态回执", sender.Sent)
	}
}

func TestBridgeCreatesJob(t *testing.T) {
	ctx := context.Background()
	aud := audit.NewMemory()
	bus := eventbus.New(10)
	empSvc := employee.NewService(employee.NewMemoryStore(), aud, bus)
	e, _ := empSvc.Create(ctx, employee.CreateInput{Name: "A", WorkstationID: "WS-1", WorkspaceID: "W1", OwnerUserID: "user-1"}, "u", "")
	st := employee.StatusActive
	_, _ = empSvc.Update(ctx, e.ID, employee.UpdateInput{Status: &st}, "u", "")
	jobSvc := job.NewService(job.NewMemoryStore(), aud, bus)
	msgSvc := message.NewService(message.NewMemoryStore(), aud)
	v, _ := secret.NewMemoryVault()
	fs := feishu.NewService(v)
	fs.UpsertBinding(feishu.Binding{EmployeeID: e.ID, FeishuAlias: "dev", FeishuOpenID: "u1"})
	sender := &feishu.MemorySender{}
	fs.Sender = sender
	notify := notification.New(fs, bus, jobSvc)
	bridge := &feishu.Bridge{
		Employees: empSvc, Jobs: jobSvc, Messages: msgSvc, Notify: notify, Feishu: fs,
	}
	bridge.Wire()
	jobID, dup, err := fs.HandleMessage(ctx, feishu.IncomingEvent{
		EventID: "e2", MessageID: "m2", ChatID: "chat", SenderOpenID: "u1", Text: "@dev fix",
	})
	if err != nil || dup || jobID == "" {
		t.Fatal(jobID, dup, err)
	}
	j, _ := jobSvc.Get(ctx, jobID)
	if j.Prompt == "" || j.CreatedBy != "user-1" {
		t.Fatal(j)
	}
	j.Status = job.StatusSuccess
	j.Result = "ok"
	_ = notify.OnJobTerminal(ctx, j)
	if len(sender.Sent) < 2 {
		t.Fatal("缺少即时确认或终态回执", sender.Sent)
	}
}

func TestUpsertReplacesAndDeleteBinding(t *testing.T) {
	v, _ := secret.NewMemoryVault()
	fs := feishu.NewService(v)
	fs.UpsertBinding(feishu.Binding{EmployeeID: "e1", FeishuAlias: "dev"})
	if got := fs.ListBindings(); len(got) != 1 || got[0].EmployeeID != "e1" {
		t.Fatal("初始绑定失败", got)
	}
	// 同一员工改别名：旧别名应失效
	fs.UpsertBinding(feishu.Binding{EmployeeID: "e1", FeishuAlias: "ops", FeishuOpenID: "ou_1"})
	if id, _, err := fs.ParseTarget("@dev hello"); err == nil && id == "e1" {
		t.Fatal("旧别名应已清除")
	}
	id, prompt, err := fs.ParseTarget("@ops hello")
	if err != nil || id != "e1" || prompt == "" {
		t.Fatal("新别名未生效", id, prompt, err)
	}
	b := fs.BindingByEmployee("e1")
	if b == nil || b.FeishuAlias != "ops" || b.FeishuOpenID != "ou_1" {
		t.Fatal("BindingByEmployee 不符", b)
	}
	if !fs.DeleteBinding("e1", "ops") {
		t.Fatal("删除应成功")
	}
	if fs.BindingByEmployee("e1") != nil || len(fs.ListBindings()) != 0 {
		t.Fatal("删除后仍存在绑定")
	}
	if fs.DeleteBinding("e1", "ops") {
		t.Fatal("重复删除应返回 false")
	}
}

func ptr(s string) *string {
	return &s
}

func TestResolveMentionsAndCleanPrompt(t *testing.T) {
	v, _ := secret.NewMemoryVault()
	s := feishu.NewService(v)
	s.UpsertBinding(feishu.Binding{EmployeeID: "EMP-83cb1d1ec3307770", FeishuAlias: "AI员工"})

	// 1. 验证 @_user_x 占位符被正确解析为真实姓名/别名
	raw := "@_user_1 @_user_2 把这个类容写入helloworld.txt中"
	mentions := []*larkim.MentionEvent{
		{Key: ptr("@_user_1"), Name: ptr("AI员工")},
		{Key: ptr("@_user_2"), Name: ptr("合并")},
	}
	resolved := s.ResolveMentions(raw, mentions)
	expectedResolved := "@AI员工 @合并 把这个类容写入helloworld.txt中"
	if resolved != expectedResolved {
		t.Fatalf("ResolveMentions 不符合预期: 得到 %q, 期望 %q", resolved, expectedResolved)
	}

	// 2. 验证 CleanPrompt 正确剥离开头的全部 @提及
	cleaned := s.CleanPrompt(resolved)
	expectedCleaned := "把这个类容写入helloworld.txt中"
	if cleaned != expectedCleaned {
		t.Fatalf("CleanPrompt 不符合预期: 得到 %q, 期望 %q", cleaned, expectedCleaned)
	}

	// 3. 验证 ParseTarget 识别目标数字员工并返回纯净指令
	empID, prompt, err := s.ParseTarget(resolved)
	if err != nil || empID != "EMP-83cb1d1ec3307770" {
		t.Fatalf("ParseTarget 失败: empID=%s, err=%v", empID, err)
	}
	if prompt != expectedCleaned {
		t.Fatalf("ParseTarget prompt 包含残留 @ 标记: %q", prompt)
	}
}

func TestQuotedMessageContextInjection(t *testing.T) {
	v, _ := secret.NewMemoryVault()
	s := feishu.NewService(v)
	s.UpsertBinding(feishu.Binding{EmployeeID: "EMP-100", FeishuAlias: "AI员工"})
	fj := &fakeJobs{}
	s.Jobs = fj
	s.Employees = fakeEmp{}
	sender := &feishu.MemorySender{}
	s.Sender = sender

	ev := feishu.IncomingEvent{
		EventID:       "ev-quote-1",
		MessageID:     "msg-child",
		ParentID:      "msg-parent-jira",
		ChatID:        "chat-grp",
		SenderOpenID:  "ou_test",
		Text:          "@AI员工 @合并 把这个类容写入helloworld.txt中",
		QuotedContent: "[JIRA] 【环境单位】环境单位漂移行走 链接: http://jira.example.com/SG-70971",
	}

	jobID, dup, err := s.HandleMessage(context.Background(), ev)
	if err != nil || dup || jobID != "JOB-1" {
		t.Fatalf("HandleMessage 失败: %v, dup=%v, jobID=%s", err, dup, jobID)
	}

	// 验证透传给 Workstation 的完整 Prompt 包含了引用父消息与纯净指令
	if len(fj.prompts) == 0 {
		t.Fatal("未创建 Job")
	}
	actualPrompt := fj.prompts[0]
	if !strings.Contains(actualPrompt, "【引用/上下文内容如下】：") ||
		!strings.Contains(actualPrompt, "[JIRA] 【环境单位】环境单位漂移行走") {
		t.Fatalf("透传工作站的 prompt 缺失引用上下文:\n%s", actualPrompt)
	}
	if !strings.Contains(actualPrompt, "【任务指令】：\n把这个类容写入helloworld.txt中") {
		t.Fatalf("透传工作站的 prompt 任务指令不正确:\n%s", actualPrompt)
	}
	if strings.Contains(actualPrompt, "@_user_1") || strings.Contains(actualPrompt, "@合并") {
		t.Fatalf("透传工作站的 prompt 依然残留 @ 标记:\n%s", actualPrompt)
	}

	// 验证回复到飞书用户的即时确认使用了消息卡片，并使用员工姓名而非 ID
	if len(sender.Sent) == 0 {
		t.Fatal("未发送飞书确认消息")
	}
	ackMsg := sender.Sent[0].Content
	if !feishu.IsCardJSON(ackMsg) {
		t.Fatalf("飞书确认回复应为消息卡片 JSON: %s", ackMsg)
	}
	if !strings.Contains(ackMsg, "AI员工") || strings.Contains(ackMsg, "EMP-100") {
		t.Fatalf("飞书确认回复中未正确展示员工姓名或残留了 EMP- ID: %s", ackMsg)
	}
	if !strings.Contains(ackMsg, "把这个类容写入helloworld.txt中") {
		t.Fatalf("飞书确认回复内容不符合预期: %s", ackMsg)
	}
	if !strings.Contains(ackMsg, "已带入引用的上下文内容") {
		t.Fatalf("飞书确认回复未标注引用上下文: %s", ackMsg)
	}
}

func TestParseMessageBodyFormats(t *testing.T) {
	// 1. 纯文本格式
	textRaw := `{"text":"hello world"}`
	if got := feishu.ParseMessageBody("text", textRaw); got != "hello world" {
		t.Fatalf("text parse failed: %s", got)
	}

	// 2. 富文本 post 格式 (含标题、链接、@)
	postRaw := `{
		"zh_cn": {
			"title": "[JIRA] 环境单位通知",
			"content": [
				[
					{"tag": "text", "text": "任务变更: "},
					{"tag": "a", "text": "SG-70971", "href": "http://jira.example.com/SG-70971"}
				],
				[
					{"tag": "text", "text": "抄送: "},
					{"tag": "at", "user_name": "周利俊"}
				]
			]
		}
	}`
	postParsed := feishu.ParseMessageBody("post", postRaw)
	if !strings.Contains(postParsed, "[JIRA] 环境单位通知") ||
		!strings.Contains(postParsed, "[SG-70971](http://jira.example.com/SG-70971)") ||
		!strings.Contains(postParsed, "@周利俊") {
		t.Fatalf("post parse failed:\n%s", postParsed)
	}

	// 3. interactive 消息卡片格式
	cardRaw := `{
		"header": {
			"title": {"content": "JIRA 告警"}
		},
		"elements": [
			{
				"tag": "div",
				"text": {"content": "状态从 (空) -> Auto bottom up"}
			}
		]
	}`
	cardParsed := feishu.ParseMessageBody("interactive", cardRaw)
	if !strings.Contains(cardParsed, "JIRA 告警") ||
		!strings.Contains(cardParsed, "状态从 (空) -> Auto bottom up") {
		t.Fatalf("card parse failed:\n%s", cardParsed)
	}
}

func TestHandleMessageRequiresExplicitMention(t *testing.T) {
	v, _ := secret.NewMemoryVault()
	s := feishu.NewService(v)
	s.UpsertBinding(feishu.Binding{EmployeeID: "EMP-可乐", FeishuAlias: "可乐"})
	fj := &fakeJobs{}
	s.Jobs = fj
	s.Employees = fakeEmp{}
	sender := &feishu.MemorySender{}
	s.Sender = sender

	// 群聊未 @：静默，不回消息、不建任务
	_, _, err := s.HandleMessage(context.Background(), feishu.IncomingEvent{
		EventID: "g1", MessageID: "mg1", ChatID: "oc_group", ChatType: "group",
		SenderOpenID: "ou_1", Text: "[JIRA] 状态变更通知",
	})
	if err != feishu.ErrNoEmployee {
		t.Fatalf("群聊未 @ 应返回 ErrNoEmployee, got %v", err)
	}
	if len(sender.Sent) != 0 {
		t.Fatalf("群聊未 @ 不应回复，实际 Sent=%d", len(sender.Sent))
	}
	if len(fj.created) != 0 {
		t.Fatalf("群聊未 @ 不应建任务")
	}

	// 群里 @ 了但别名/格式不对：要回格式提示，不能当没看见
	_, _, err = s.HandleMessage(context.Background(), feishu.IncomingEvent{
		EventID: "g2", MessageID: "mg2", ChatID: "oc_group", ChatType: "group",
		SenderOpenID: "ou_1", Text: "@不存在的员工 帮我看看",
	})
	if err != feishu.ErrNoEmployee {
		t.Fatalf("错误别名应返回 ErrNoEmployee, got %v", err)
	}
	if len(sender.Sent) != 1 || !strings.Contains(sender.Sent[0].Content, "请") {
		t.Fatalf("群聊格式错误应回复提示，实际 %#v", sender.Sent)
	}
	if len(fj.created) != 0 {
		t.Fatalf("格式错误不应建任务")
	}

	// 私聊未 @：只回提示卡，不建任务
	_, _, err = s.HandleMessage(context.Background(), feishu.IncomingEvent{
		EventID: "p1", MessageID: "mp1", ChatID: "oc_p2p", ChatType: "p2p",
		SenderOpenID: "ou_1", Text: "帮我看看这个问题",
	})
	if err != feishu.ErrNoEmployee {
		t.Fatalf("私聊未 @ 应返回 ErrNoEmployee, got %v", err)
	}
	if len(sender.Sent) != 2 {
		t.Fatalf("私聊未 @ 应再回复 1 条提示，实际 Sent=%d", len(sender.Sent))
	}
	if !strings.Contains(sender.Sent[1].Content, "请@对应员工执行") &&
		!strings.Contains(sender.Sent[1].Content, "@对应员工") {
		t.Fatalf("提示卡内容不符合预期: %s", sender.Sent[1].Content)
	}
	if len(fj.created) != 0 {
		t.Fatalf("私聊未 @ 不应建任务")
	}

	// 私聊显式 @：正常派单
	jobID, dup, err := s.HandleMessage(context.Background(), feishu.IncomingEvent{
		EventID: "p2", MessageID: "mp2", ChatID: "oc_p2p", ChatType: "p2p",
		SenderOpenID: "ou_1", Text: "@可乐 帮我看看这个问题",
	})
	if err != nil || dup || jobID != "JOB-1" {
		t.Fatalf("私聊 @ 派单失败: job=%s dup=%v err=%v", jobID, dup, err)
	}
	if len(fj.created) != 1 {
		t.Fatalf("私聊 @ 应建 1 个任务, got %d", len(fj.created))
	}
}

func TestAutoBindFromMention(t *testing.T) {
	v, _ := secret.NewMemoryVault()
	s := feishu.NewService(v)
	s.UpsertBinding(feishu.Binding{EmployeeID: "e1", FeishuAlias: "可乐"})

	p2p := s.AutoBindFromMention("e1", "ou_user", "oc_p2p", false)
	if !p2p.OpenBoundNow || p2p.ChatBoundNow {
		t.Fatalf("单聊应只绑定 OpenID: %+v", p2p)
	}
	if b := s.BindingByEmployee("e1"); b == nil || b.FeishuOpenID != "ou_user" || b.ChatID != "" {
		t.Fatalf("单聊写入不符: %+v", b)
	}
	again := s.AutoBindFromMention("e1", "ou_user", "oc_p2p", false)
	if again.OpenBoundNow || !strings.Contains(feishu.FormatIdentityNotice(again), "ou_user") {
		t.Fatalf("已绑定仍应告知 OpenID: %+v %s", again, feishu.FormatIdentityNotice(again))
	}

	grp := s.AutoBindFromMention("e1", "ou_user", "oc_group", true)
	if !grp.ChatBoundNow {
		t.Fatalf("群聊应补默认群: %+v", grp)
	}
	if b := s.BindingByEmployee("e1"); b == nil || b.ChatID != "oc_group" || b.FeishuOpenID != "ou_user" {
		t.Fatalf("群聊写入不符: %+v", b)
	}
	note := feishu.FormatIdentityNotice(s.AutoBindFromMention("e1", "ou_user", "oc_group", true))
	if !strings.Contains(note, "ou_user") || !strings.Contains(note, "oc_group") || strings.Contains(note, "本次已自动绑定") {
		t.Fatalf("已绑定回执应带两个 ID 且不再标本次绑定: %s", note)
	}
}

func TestAutoBindSameOpenIDOnTwoEmployees(t *testing.T) {
	v, _ := secret.NewMemoryVault()
	s := feishu.NewService(v)
	s.UpsertBinding(feishu.Binding{EmployeeID: "e1", FeishuAlias: "test"})
	s.UpsertBinding(feishu.Binding{EmployeeID: "e2", FeishuAlias: "test2"})

	n1 := s.AutoBindFromMention("e1", "ou_shared", "oc_p2p", false)
	n2 := s.AutoBindFromMention("e2", "ou_shared", "oc_p2p", false)
	if !n1.OpenBoundNow || !n2.OpenBoundNow {
		t.Fatalf("两个员工都应自动绑定同一 OpenID: %+v %+v", n1, n2)
	}
	b1 := s.BindingByEmployee("e1")
	b2 := s.BindingByEmployee("e2")
	if b1 == nil || b2 == nil || b1.FeishuOpenID != "ou_shared" || b2.FeishuOpenID != "ou_shared" {
		t.Fatalf("绑定结果不符: %+v %+v", b1, b2)
	}
	if !s.EmployeeHasOpenID("e1", "ou_shared") || !s.EmployeeHasOpenID("e2", "ou_shared") {
		t.Fatal("同一飞书用户应同时关联两个员工")
	}
}

func TestResolveTargetChatAndUserName(t *testing.T) {
	v, _ := secret.NewMemoryVault()
	s := feishu.NewService(v)

	// 1. 无绑定时，返回空
	if target := s.ResolveTargetChat("e1"); target != "" {
		t.Fatalf("无绑定期望空，得到 %s", target)
	}

	// 2. 仅有 ChatID，返回 ChatID
	s.UpsertBinding(feishu.Binding{EmployeeID: "e1", FeishuAlias: "bot", ChatID: "oc_group_1"})
	if target := s.ResolveTargetChat("e1"); target != "oc_group_1" {
		t.Fatalf("期望 oc_group_1，得到 %s", target)
	}

	// 3. 有 OpenID 时，优先返回 OpenID
	s.UpsertBinding(feishu.Binding{EmployeeID: "e1", FeishuAlias: "bot", FeishuOpenID: "ou_user_1", ChatID: "oc_group_1"})
	if target := s.ResolveTargetChat("e1"); target != "ou_user_1" {
		t.Fatalf("期望 ou_user_1，得到 %s", target)
	}

	// 4. 姓名缓存与解析
	s.CacheUserName("ou_user_1", "张小三")
	if name := s.ResolveUserName(context.Background(), "ou_user_1"); name != "张小三" {
		t.Fatalf("期望获取缓存姓名 张小三，得到 %s", name)
	}
	if name := s.ResolveUserName(context.Background(), "ou_unknown"); name != "" {
		t.Fatalf("未知用户未连接真实接口时期望空，得到 %s", name)
	}
}

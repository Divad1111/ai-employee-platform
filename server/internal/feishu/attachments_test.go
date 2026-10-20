package feishu_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	input "github.com/ai-employee-platform/gen/jobinput"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/jobinput"
	"github.com/ai-employee-platform/server/internal/message"
	"github.com/ai-employee-platform/server/internal/taskconversation"
	lark "github.com/larksuite/oapi-sdk-go/v3"
)

func TestAttachmentStageIsolationAndJobPersistence(t *testing.T) {
	ctx := context.Background()
	fs, sender, employees, _, _ := commandFixture(t)
	files := &jobinput.Store{Root: t.TempDir()}
	fs.InputFiles = files
	jobs := job.NewService(job.NewMemoryStore(), audit.NewMemory(), eventbus.New(20))
	conversations := taskconversation.NewService(taskconversation.NewMemoryStore(), fs, jobs, nil)
	fs.SetTaskConversationHandler(conversations)
	fs.Jobs = &feishu.Bridge{Employees: employees, Jobs: jobs, Messages: message.NewService(message.NewMemoryStore(), audit.NewMemory()), Feishu: fs, Conversations: conversations}
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jE/kAAAAASUVORK5CYII=")
	resources := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/tenant_access_token/") {
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "tenant_access_token": "test-token", "expire": 7200})
			return
		}
		if r.URL.Path == "/open-apis/im/v1/messages/om-quoted-file" {
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"items": []any{map[string]any{"message_id": "om-quoted-file", "chat_id": "chat", "msg_type": "file", "body": map[string]any{"content": `{"file_key":"quoted-key","file_name":"引用文档.pdf"}`}}}}})
			return
		}
		if strings.Contains(r.URL.Path, "/resources/") {
			if strings.Contains(r.URL.Path, "denied-key") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(403)
				w.Write([]byte(`{"code":99991672,"msg":"denied"}`))
				return
			}
			resources++
			if r.URL.Query().Get("type") == "image" {
				w.Header().Set("Content-Type", "image/png")
				w.Write(png)
			} else {
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Write([]byte("file contents"))
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer api.Close()
	fs.SetLarkClient(lark.NewClient("test-app-attachments", "test", lark.WithOpenBaseUrl(api.URL)))
	stage := feishu.IncomingEvent{ChatID: "chat", ChatType: "p2p", SenderOpenID: "owner", EventID: "image", MessageType: "image", Attachments: []feishu.Attachment{{MessageID: "om-image", Key: "img-key", Kind: "image", Name: "image.png"}}}
	if _, _, err := fs.HandleMessage(ctx, stage); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ chat, sender, want string }{{"chat", "stranger", "没有待派单"}, {"other-chat", "owner", "没有待派单"}, {"chat", "owner", "image.png"}} {
		_, _, _ = fs.HandleMessage(ctx, feishu.IncomingEvent{ChatID: tc.chat, SenderOpenID: tc.sender, Text: "/files"})
		if !strings.Contains(sender.Sent[len(sender.Sent)-1].Content, tc.want) {
			t.Fatal("pending files leaked across sender/chat")
		}
	}
	_, _, _ = fs.HandleMessage(ctx, feishu.IncomingEvent{ChatID: "chat", SenderOpenID: "owner", Text: "/help"})
	stage.EventID, stage.MessageType = "file", "file"
	stage.Attachments = []feishu.Attachment{{MessageID: "om-file", Key: "file-key", Kind: "file", Name: "需求文档.pdf"}}
	_, _, _ = fs.HandleMessage(ctx, stage)
	id, _, err := fs.HandleMessage(ctx, feishu.IncomingEvent{ChatID: "chat", SenderOpenID: "owner", EventID: "task", MessageID: "om-task", Text: "@test3 阅读附件"})
	if err != nil || id == "" {
		t.Fatalf("job creation failed: %v, last reply %s", err, sender.Sent[len(sender.Sent)-1].Content)
	}
	j, _ := jobs.Get(ctx, id)
	if len(j.InputAttachments) != 2 || resources != 2 {
		t.Fatal("attachments were not saved with the job")
	}
	for _, attachment := range j.InputAttachments {
		reader, err := files.Open(attachment.SHA256)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(reader)
		reader.Close()
		if !bytes.Equal(data, png) && string(data) != "file contents" {
			t.Fatal("file bytes changed")
		}
	}
	_, _, _ = fs.HandleMessage(ctx, feishu.IncomingEvent{ChatID: "chat", SenderOpenID: "owner", Text: "/files"})
	if !strings.Contains(sender.Sent[len(sender.Sent)-1].Content, "没有待派单") {
		t.Fatal("successful task did not consume pending files")
	}
	if !strings.Contains(sender.Sent[len(sender.Sent)-1].Content, j.ID) || !strings.Contains(sender.Sent[len(sender.Sent)-1].Content, "需求文档.pdf") {
		t.Fatal("consumed attachments disappeared from /files history")
	}
	// A new service has an empty pending buffer but can read persisted manifests.
	fs.SetTaskConversationHandler(taskconversation.NewService(conversations.Store, fs, jobs, nil))
	fs.HandleMessage(ctx, feishu.IncomingEvent{ChatID: "chat", SenderOpenID: "owner", Text: "/clear files"})
	fs.HandleMessage(ctx, feishu.IncomingEvent{ChatID: "chat", SenderOpenID: "owner", Text: "/files"})
	if !strings.Contains(sender.Sent[len(sender.Sent)-1].Content, j.ID) {
		t.Fatal("clear/restart removed persisted attachment history")
	}
	for _, ev := range []feishu.IncomingEvent{{ChatID: "chat", SenderOpenID: "stranger", Text: "/files"}, {ChatID: "other-chat", SenderOpenID: "owner", Text: "/files"}} {
		fs.HandleMessage(ctx, ev)
		if strings.Contains(sender.Sent[len(sender.Sent)-1].Content, j.ID) || strings.Contains(sender.Sent[len(sender.Sent)-1].Content, "需求文档.pdf") {
			t.Fatal("attachment history leaked across sender/chat")
		}
	}
	// History is never silently reused as input for the next task.
	nextID, _, nextErr := fs.HandleMessage(ctx, feishu.IncomingEvent{ChatID: "chat", SenderOpenID: "owner", MessageID: "om-no-files", Text: "@test3 新的纯文本任务"})
	nextJob, getErr := jobs.Get(ctx, nextID)
	if nextErr != nil || getErr != nil || len(nextJob.InputAttachments) != 0 {
		t.Fatal("attachment history was automatically reused")
	}
	// Real mixed-post shape: image rows and a sibling files array. Both resources
	// must survive SDK downloads and job persistence, despite duplicated v2 rows.
	raw := `{"content":[[{"tag":"img","image_key":"mixed-image"}],[{"tag":"text","text":"@test3 文件和图片上的内容是什么，总结一下"}]],"content_v2":[[{"tag":"img","image_key":"mixed-image"}],[{"tag":"text","text":"@test3 文件和图片上的内容是什么，总结一下"}]],"files":[{"file_key":"mixed-txt","file_name":"test.txt","is_folder":false}],"title":""}`
	beforeResources := resources
	mixedID, _, mixedErr := fs.HandleMessage(ctx, feishu.IncomingEvent{ChatID: "chat", SenderOpenID: "owner", MessageID: "om-mixed", MessageType: "post", Text: feishu.ParseMessageBody("post", raw), Attachments: feishu.ParseAttachments("post", raw, "om-mixed")})
	mixed, getErr := jobs.Get(ctx, mixedID)
	if mixedErr != nil || getErr != nil || len(mixed.InputAttachments) != 2 || resources-beforeResources != 2 || mixed.InputAttachments[1].Name != "test.txt" {
		t.Fatalf("mixed post lost a file or downloaded duplicate resources: job=%+v err=%v/%v", mixed, mixedErr, getErr)
	}
	reader, err := files.Open(mixed.InputAttachments[1].SHA256)
	if err != nil {
		t.Fatal(err)
	}
	txt, _ := io.ReadAll(reader)
	reader.Close()
	if string(txt) != "file contents" {
		t.Fatal("mixed-post text file content was not stored")
	}
	// A reply can restore a file after the pending buffer has been consumed or
	// after a control-plane restart. The referenced message must match this chat.
	id, _, err = fs.HandleMessage(ctx, feishu.IncomingEvent{ChatID: "chat", SenderOpenID: "owner", EventID: "quoted-task", MessageID: "om-task-2", ParentID: "om-quoted-file", Text: "@test3 总结引用文件"})
	if err != nil || id == "" {
		t.Fatalf("reply-to-file failed: %v, reply: %s", err, sender.Sent[len(sender.Sent)-1].Content)
	}
	j, _ = jobs.Get(ctx, id)
	if len(j.InputAttachments) != 1 || j.InputAttachments[0].Name != "引用文档.pdf" {
		t.Fatal("quoted file was not attached")
	}
	// The same quoted ID from another chat must not be downloaded.
	before, _ := jobs.List(ctx)
	fs.HandleMessage(ctx, feishu.IncomingEvent{ChatID: "other-chat", SenderOpenID: "owner", EventID: "wrong-chat", MessageID: "om-wrong-chat", ParentID: "om-quoted-file", Text: "@test3 阅读引用文件"})
	after, _ := jobs.List(ctx)
	if len(before) != len(after) {
		t.Fatal("cross-chat quoted message created a task")
	}
	// Failed downloads retain pending references and never create a file-less Job.
	stage.EventID = "denied-file"
	stage.Attachments = []feishu.Attachment{{MessageID: "om-denied", Key: "denied-key", Kind: "file", Name: "denied.pdf"}}
	fs.HandleMessage(ctx, stage)
	fs.HandleMessage(ctx, feishu.IncomingEvent{ChatID: "chat", SenderOpenID: "owner", EventID: "denied-task", MessageID: "om-denied-task", Text: "@test3 阅读附件"})
	after, _ = jobs.List(ctx)
	if len(before) != len(after) || !strings.Contains(sender.Sent[len(sender.Sent)-1].Content, "附件准备失败") {
		t.Fatal("failed resource download created a task or lost error")
	}
	fs.HandleMessage(ctx, feishu.IncomingEvent{ChatID: "chat", SenderOpenID: "owner", Text: "/files"})
	if !strings.Contains(sender.Sent[len(sender.Sent)-1].Content, "denied.pdf") {
		t.Fatal("failed task consumed pending file")
	}
}

func TestMixedPostResourceLayouts(t *testing.T) {
	for _, raw := range []string{
		`{"files":[{"file_key":"txt","file_name":"test.txt"}],"content":[[{"tag":"img","image_key":"pic"}],[{"tag":"text","text":"@test3 总结"}]],"content_v2":[[{"tag":"img","image_key":"pic"}]]}`,
		`{"zh_cn":{"content":[[{"tag":"img","image_key":"pic"},{"tag":"text","text":"@test3 总结"}]]},"files":[{"file_key":"txt","file_name":"test.txt"}]}`,
		`{"en_us":{"content_v2":[[{"tag":"img","image_key":"pic"},{"tag":"file","file_key":"txt","file_name":"test.txt"},{"tag":"text","text":"@test3 总结"}]],"files":[{"file_key":"txt","file_name":"test.txt"}]}}`,
		`{"content_v2":[[{"tag":"img","image_key":"pic"},{"tag":"text","text":"@test3 总结"}]],"files":[{"file_key":"txt","file_name":"test.txt"},{"file_key":"folder","is_folder":true}]}`,
	} {
		files := feishu.ParseAttachments("post", raw, "om-post")
		byKey := map[string]feishu.Attachment{}
		for _, f := range files {
			byKey[f.Key] = f
		}
		if len(files) != 2 || byKey["pic"].Kind != "image" || byKey["txt"].Name != "test.txt" {
			t.Fatalf("resources=%+v raw=%s", files, raw)
		}
		if text := feishu.ParseMessageBody("post", raw); text != "@test3 总结" {
			t.Fatalf("prompt=%q", text)
		}
	}
	files := feishu.ParseAttachments("post", `{"content":[],"files":[{"file_key":"a","file_name":"a.txt"},{"file_key":"b","file_name":"b.txt"}],"card":{"files":[{"file_key":"not-a-message-resource"}]}}`, "om-files")
	if len(files) != 2 || files[0].Name != "a.txt" || files[1].Name != "b.txt" {
		t.Fatal(files)
	}
}

func TestAttachmentHistoryOrderAndLimit(t *testing.T) {
	ctx := context.Background()
	store := taskconversation.NewMemoryStore()
	jobs := job.NewService(job.NewMemoryStore(), audit.NewMemory(), eventbus.New(20))
	service := taskconversation.NewService(store, nil, jobs, nil)
	for i := 0; i < 7; i++ {
		// Use an actual manifest generated by the content store.
		content := &jobinput.Store{Root: t.TempDir()}
		f, err := content.Put("history.txt", strings.NewReader("history"))
		if err != nil {
			t.Fatal(err)
		}
		j, _, err := jobs.Create(ctx, job.CreateInput{EmployeeID: "EMP-test", Prompt: "attachment", IdempotencyKey: fmt.Sprint("history-", i), InputAttachments: []input.File{f}}, "user", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := store.CreateTask(ctx, &taskconversation.Conversation{ID: j.ID, CurrentJobID: j.ID, ChatID: "chat", OwnerOpenID: "owner", CreatedAt: time.Unix(int64(i), 0)}, "attachment", 0); err != nil {
			t.Fatal(err)
		}
	}
	all, err := store.ListOwnerChatIDs(ctx, "chat", "owner", 100)
	history, historyErr := service.RecentAttachmentJobs(ctx, "chat", "owner", 50)
	if err != nil || historyErr != nil || len(history) != 5 || history[0].JobID != all[0] || history[4].JobID != all[4] {
		t.Fatalf("history=%+v err=%v/%v", history, err, historyErr)
	}
	for _, pair := range [][2]string{{"chat", ""}, {"", "owner"}, {"chat", "stranger"}, {"other", "owner"}} {
		got, err := service.RecentAttachmentJobs(ctx, pair[0], pair[1], 5)
		if err != nil || len(got) != 0 {
			t.Fatal("history identity boundary failed", got, err)
		}
	}
}

func TestPostAttachmentExtractionAndClear(t *testing.T) {
	files := feishu.ParseAttachments("post", `{"zh_cn":{"content":[[{"tag":"text","text":"@test3 描述图片"},{"tag":"img","image_key":"img1"},{"tag":"img","image_key":"img1"}]]}}`, "om-post")
	if len(files) != 1 || files[0].MessageID != "om-post" || files[0].Key != "img1" {
		t.Fatal(files)
	}
	fs, sender, _, _, _ := commandFixture(t)
	ev := feishu.IncomingEvent{ChatID: "chat", ChatType: "p2p", SenderOpenID: "owner", MessageType: "image", Attachments: files}
	fs.HandleMessage(context.Background(), ev)
	fs.HandleMessage(context.Background(), feishu.IncomingEvent{ChatID: "chat", SenderOpenID: "owner", Text: "/clear files"})
	fs.HandleMessage(context.Background(), feishu.IncomingEvent{ChatID: "chat", SenderOpenID: "owner", Text: "/files"})
	if !strings.Contains(sender.Sent[len(sender.Sent)-1].Content, "没有待派单") {
		t.Fatal("clear did not remove own pending files")
	}
}

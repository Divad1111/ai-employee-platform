package feishu_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/message"
	"github.com/ai-employee-platform/server/internal/notification"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/secret"
)

type fakeJobs struct {
	created []string
}

func (f *fakeJobs) CreateFromFeishu(_ context.Context, employeeID, prompt, idem, chat, msg string) (string, error) {
	f.created = append(f.created, employeeID+"|"+idem)
	return "JOB-1", nil
}

type fakeEmp struct{}

func (fakeEmp) ResolveAlias(context.Context, string) (string, error) { return "", feishu.ErrNoEmployee }
func (fakeEmp) IsAssignable(context.Context, string) error           { return nil }

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
	if len(sender.Sent) != 1 {
		t.Fatal(sender.Sent)
	}
}

func TestBridgeCreatesJob(t *testing.T) {
	ctx := context.Background()
	aud := audit.NewMemory()
	bus := eventbus.New(10)
	empSvc := employee.NewService(employee.NewMemoryStore(), aud, bus)
	e, _ := empSvc.Create(ctx, employee.CreateInput{Name: "A", WorkstationID: "WS-1", WorkspaceID: "W1"}, "u", "")
	st := employee.StatusActive
	_, _ = empSvc.Update(ctx, e.ID, employee.UpdateInput{Status: &st}, "u", "")
	jobSvc := job.NewService(job.NewMemoryStore(), aud, bus)
	msgSvc := message.NewService(message.NewMemoryStore(), aud)
	v, _ := secret.NewMemoryVault()
	fs := feishu.NewService(v)
	fs.UpsertBinding(feishu.Binding{EmployeeID: e.ID, FeishuAlias: "dev"})
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
	if j.Prompt == "" {
		t.Fatal(j)
	}
	j.Status = job.StatusSuccess
	j.Result = "ok"
	_ = notify.OnJobTerminal(ctx, j)
	if len(sender.Sent) != 1 {
		t.Fatal("应回复飞书")
	}
}

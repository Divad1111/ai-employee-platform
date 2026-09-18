package approval_test

import (
	"context"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/approval"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/permission"
	"github.com/ai-employee-platform/server/internal/secret"
	"github.com/ai-employee-platform/server/internal/totp"
)

func setup(t *testing.T) (*approval.Service, *job.Service, *audit.Memory, string) {
	t.Helper()
	ctx := context.Background()
	aud := audit.NewMemory()
	bus := eventbus.New(20)
	jobSvc := job.NewService(job.NewMemoryStore(), aud, bus)
	permStore := permission.NewMemoryStore()
	_ = permission.EnsureDefault(permStore)
	eng := permission.NewEngine(permStore, aud)
	vault, _ := secret.NewMemoryVault()
	svc := approval.New(approval.NewMemoryStore(), approval.NewMemoryTOTP(), vault, jobSvc, eng, aud, bus)

	j, _, err := jobSvc.Create(ctx, job.CreateInput{
		EmployeeID: "EMP-1", WorkspaceID: "W1", Prompt: "p", IdempotencyKey: "k-apr",
	}, "u", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{job.StatusQueued, job.StatusAssigned, job.StatusStarting, job.StatusRunning} {
		if _, err := jobSvc.Transition(ctx, j.ID, st, "u", "", nil); err != nil {
			t.Fatal(st, err)
		}
	}
	return svc, jobSvc, aud, j.ID
}

func TestCriticalWithoutTOTPDeny(t *testing.T) {
	svc, _, _, jobID := setup(t)
	d, ar, err := svc.EvaluateAndMaybeCreate(context.Background(), permission.Request{
		ActorType: "EMPLOYEE", ActorID: "EMP-1", Action: permission.ActionGitPush, JobID: jobID,
	}, "admin-user")
	if err != approval.ErrTOTPNotConfigured {
		t.Fatalf("err=%v", err)
	}
	if d.Effect != permission.EffectDeny {
		t.Fatal(d)
	}
	if ar == nil || ar.Status != approval.StatusDenied {
		t.Fatal(ar)
	}
}

func TestGitPushAskThenApproveWithTOTP(t *testing.T) {
	svc, jobSvc, _, jobID := setup(t)
	ctx := context.Background()
	userID := "admin-1"
	sec, _, err := svc.EnrollTOTP(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	d, ar, err := svc.EvaluateAndMaybeCreate(ctx, permission.Request{
		ActorType: "EMPLOYEE", ActorID: "EMP-1", Action: permission.ActionGitPush, JobID: jobID,
	}, userID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Effect != permission.EffectAsk || ar.Status != approval.StatusPending {
		t.Fatal(d, ar)
	}
	j, _ := jobSvc.Get(ctx, jobID)
	if j.Status != job.StatusWaitingApproval {
		t.Fatal(j.Status)
	}
	code, err := totp.CodeAt(sec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(ctx, ar.ID, userID, "", ""); err != approval.ErrTOTPRequired {
		t.Fatalf("无码应要求 TOTP: %v", err)
	}
	got, err := svc.Approve(ctx, ar.ID, userID, code, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != approval.StatusApproved {
		t.Fatal(got.Status)
	}
	j, _ = jobSvc.Get(ctx, jobID)
	if j.Status != job.StatusRunning {
		t.Fatal(j.Status)
	}
	ev, _ := jobSvc.Timeline(ctx, jobID)
	found := false
	for _, e := range ev {
		if e.EventType == "APPROVAL_GRANTED" {
			found = true
		}
	}
	if !found {
		t.Fatal("缺少 APPROVAL_GRANTED")
	}
}

func TestRejectApprovalMarksJobFailed(t *testing.T) {
	svc, jobSvc, _, jobID := setup(t)
	ctx := context.Background()
	userID := "admin-2"
	_, _, err := svc.EnrollTOTP(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	d, ar, err := svc.EvaluateAndMaybeCreate(ctx, permission.Request{
		ActorType: "EMPLOYEE", ActorID: "EMP-1", Action: permission.ActionFSDeleteBulk, JobID: jobID,
	}, userID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Effect != permission.EffectAsk {
		t.Fatal(d)
	}
	got, err := svc.Reject(ctx, ar.ID, userID, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != approval.StatusRejected {
		t.Fatal(got.Status)
	}
	j, _ := jobSvc.Get(ctx, jobID)
	if j.Status != job.StatusFailed {
		t.Fatal(j.Status)
	}
	ev, _ := jobSvc.Timeline(ctx, jobID)
	found := false
	for _, e := range ev {
		if e.EventType == "APPROVAL_REJECTED" {
			found = true
		}
	}
	if !found {
		t.Fatal("缺少 APPROVAL_REJECTED")
	}
}

func TestTOTPLockAfterFailures(t *testing.T) {
	svc, _, aud, _ := setup(t)
	ctx := context.Background()
	userID := "lock-user"
	_, _, err := svc.EnrollTOTP(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		err := svc.VerifyUserTOTP(ctx, userID, "999999", "ip")
		if err != approval.ErrTOTPInvalid {
			t.Fatalf("第 %d 次应 Invalid: %v", i+1, err)
		}
	}
	if err := svc.VerifyUserTOTP(ctx, userID, "999999", "ip"); err != approval.ErrTOTPLocked {
		t.Fatalf("应锁定: %v", err)
	}
	fails := aud.Query(audit.Filter{Action: "approval.totp", Limit: 20})
	if len(fails) < 5 {
		t.Fatalf("应有失败审计: %d", len(fails))
	}
}

package taskconversation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/secret"
	lark "github.com/larksuite/oapi-sdk-go/v3"
)

type cardRoundTripper func(*http.Request) (*http.Response, error)

func (f cardRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func cardResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func cardTestService(t *testing.T, transport cardRoundTripper) *Service {
	t.Helper()
	vault, _ := secret.NewMemoryVault()
	ref, _ := vault.Put("app-secret", "fake-secret")
	fs := feishu.NewService(vault)
	fs.Gateway = nil // No real Feishu connection or messages in this test.
	fs.SetConfig(feishu.Config{AppID: t.Name(), AppSecretRef: ref.ID, Enabled: true})
	fs.SetLarkClient(lark.NewClient(t.Name(), "fake-secret", lark.WithHttpClient(&http.Client{Transport: transport})))
	jobs := job.NewService(job.NewMemoryStore(), audit.NewMemory(), eventbus.New(20))
	return NewService(NewMemoryStore(), fs, jobs, nil)
}

func createCardTask(t *testing.T, s *Service, id, chat string) *Conversation {
	t.Helper()
	j, _, err := s.Jobs.Create(context.Background(), job.CreateInput{EmployeeID: "EMP", IdempotencyKey: id, Prompt: "hello"}, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	c := &Conversation{ID: id, CurrentJobID: j.ID, EmployeeID: "EMP", ChatID: chat, Status: job.StatusRunning, CreatedAt: time.Now().Add(-time.Minute)}
	if err := s.Store.CreateTask(context.Background(), c, "hello", 0); err != nil {
		t.Fatal(err)
	}
	return c
}

func awaitCard(t *testing.T, ready func() bool) {
	t.Helper()
	timer, ticker := time.NewTimer(2*time.Second), time.NewTicker(5*time.Millisecond)
	defer timer.Stop()
	defer ticker.Stop()
	for {
		if ready() {
			return
		}
		select {
		case <-timer.C:
			t.Fatal("card operation did not complete")
		case <-ticker.C:
		}
	}
}

func TestSlowCardDoesNotBlockNewReceiptAndCannotDuplicateInFlight(t *testing.T) {
	slowStarted, fastSent, slowFinished := make(chan struct{}, 1), make(chan struct{}, 1), make(chan struct{}, 1)
	var slowRequests atomic.Int32
	var fastKey atomic.Value
	s := cardTestService(t, func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			return cardResponse(`{"code":0,"tenant_access_token":"test-token","expire":7200}`), nil
		}
		if r.Method == http.MethodPatch {
			return cardResponse(`{"code":0}`), nil
		}
		var body struct {
			ReceiveID string `json:"receive_id"`
			UUID      string `json:"uuid"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.ReceiveID == "oc-slow" {
			slowRequests.Add(1)
			slowStarted <- struct{}{}
			<-r.Context().Done()
			slowFinished <- struct{}{}
			return nil, r.Context().Err()
		}
		fastKey.Store(body.UUID)
		fastSent <- struct{}{}
		return cardResponse(`{"code":0,"data":{"message_id":"om-fast","chat_id":"oc-fast"}}`), nil
	})
	s.refreshTimeout = time.Second
	slow, fast := createCardTask(t, s, "TC-slow", "oc-slow"), createCardTask(t, s, "TC-fast", "oc-fast")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.markDirty(slow.ID, 0)
	s.flushDue(ctx)
	select {
	case <-slowStarted:
	case <-time.After(time.Second):
		t.Fatal("slow request never started")
	}
	s.markDirty(slow.ID, 0)
	s.markDirty(fast.ID, 0)
	for i := 0; i < 5; i++ {
		s.flushDue(ctx)
	}
	select {
	case <-fastSent:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("slow card blocked a new receipt")
	}
	awaitCard(t, func() bool { c, _ := s.Store.Get(ctx, fast.ID); return c.CardMessageID == "om-fast" })
	if slowRequests.Load() != 1 || fastKey.Load() != "TC-fast" {
		t.Fatal("duplicate in-flight create or missing idempotency key")
	}
	select {
	case <-slowFinished:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked network request has no deadline")
	}
	if err := s.refreshCard(ctx, fast.ID); err != nil {
		t.Fatal(err)
	}
	events, _ := s.Jobs.ListEvents(ctx, fast.CurrentJobID)
	receipts := 0
	for _, event := range events {
		if event.EventType == "FEISHU_RECEIPT" {
			receipts++
		}
	}
	if receipts != 1 {
		t.Fatalf("receipt must be recorded once, got %d", receipts)
	}
}

func TestActiveCardsHaveHeartbeatAndTerminalCardsStop(t *testing.T) {
	s := NewService(NewMemoryStore(), nil, nil, nil)
	c := &Conversation{ID: "TC-active", CurrentJobID: "JOB-active", EmployeeID: "EMP", Status: job.StatusRunning}
	s.Store.CreateTask(context.Background(), c, "hello", 0)
	s.trackActive(c.ID, true)
	s.markActiveDirty()
	if _, exists := s.dirty[c.ID]; !exists {
		t.Fatal("active card has no heartbeat")
	}
	s.OnJobStatus(context.Background(), c.CurrentJobID, job.StatusSuccess, "done")
	s.dirty = map[string]time.Time{}
	s.markActiveDirty()
	if len(s.dirty) != 0 {
		t.Fatal("completed card still has a heartbeat")
	}
}

func TestCardShowsReceivedDurationWithoutInventingModelActivity(t *testing.T) {
	now := time.Now()
	c := &Conversation{ID: "TC", CurrentJobID: "JOB", Status: job.StatusRunning, CreatedAt: now.Add(-66 * time.Second)}
	text := buildCardAt(c, nil, nil, now).MustJSON()
	if !strings.Contains(text, "66 秒前") || !strings.Contains(text, "无需重复发送") {
		t.Fatal("slow model has no visible receipt/waiting feedback")
	}
	c.Status = job.StatusSuccess
	if strings.Contains(buildCardAt(c, nil, nil, now).MustJSON(), "无需重复发送") {
		t.Fatal("finished card still claims to be waiting")
	}
}

func TestTerminalUpdateDuringReceiptDeliveryIsNotLost(t *testing.T) {
	creating, release, patched := make(chan struct{}, 1), make(chan struct{}), make(chan struct{}, 1)
	var creates atomic.Int32
	s := cardTestService(t, func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			return cardResponse(`{"code":0,"tenant_access_token":"test-token","expire":7200}`), nil
		}
		if r.Method == http.MethodPatch {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "已完成") {
				t.Error("terminal status missing from updated card")
			}
			patched <- struct{}{}
			return cardResponse(`{"code":0}`), nil
		}
		creates.Add(1)
		creating <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return cardResponse(`{"code":0,"data":{"message_id":"om-receipt","chat_id":"oc-chat"}}`), nil
	})
	c := createCardTask(t, s, "TC-update", "oc-chat")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.markDirty(c.ID, 0)
	s.flushDue(ctx)
	select {
	case <-creating:
	case <-time.After(time.Second):
		t.Fatal("receipt did not start")
	}
	s.OnJobStatus(ctx, c.CurrentJobID, job.StatusSuccess, "done")
	s.flushDue(ctx)
	close(release)
	awaitCard(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); return !s.inFlight[c.ID] })
	s.flushDue(ctx)
	select {
	case <-patched:
	case <-time.After(time.Second):
		t.Fatal("terminal update was dropped during in-flight receipt")
	}
	if creates.Load() != 1 {
		t.Fatal("terminal update created a duplicate receipt")
	}
}

func TestRestartRestoresTerminalCardThatMightHaveMissedFinalPatch(t *testing.T) {
	patched := make(chan struct{}, 1)
	s := cardTestService(t, func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			return cardResponse(`{"code":0,"tenant_access_token":"test-token","expire":7200}`), nil
		}
		if r.Method == http.MethodPatch {
			patched <- struct{}{}
			return cardResponse(`{"code":0}`), nil
		}
		t.Error("completed card was recreated")
		return cardResponse(`{"code":1}`), nil
	})
	c := createCardTask(t, s, "TC-restore", "oc-chat")
	s.Store.SetTurnStatus(context.Background(), c.CurrentJobID, job.StatusSuccess)
	s.Store.SetCardMessageID(context.Background(), c.ID, "om-existing")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan struct{})
	go func() { defer close(stopped); s.Run(ctx) }()
	select {
	case <-patched:
	case <-time.After(2 * time.Second):
		t.Fatal("restart did not restore final card state")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("card worker did not stop")
	}
}

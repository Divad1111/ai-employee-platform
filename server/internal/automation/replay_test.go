package automation

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSignedWebhookReplayIgnoresUnsignedHeaders(t *testing.T) {
	svc, _, _ := newTestSvc(t)
	_, secrets, cfg := createWebhook(t, svc, `{"auth_modes":["hmac"],"rate_limit_per_min":60}`)
	ts, body := time.Now().UTC().Format(time.RFC3339), "{}"
	signature := signBody(secrets["hmac_secret"], ts, body)
	request := func(key, sig string) error {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("X-AIE-Timestamp", ts)
		r.Header.Set("X-AIE-Signature", sig)
		r.Header.Set("X-Idempotency-Key", key)
		_, _, err := svc.HandleWebhook(context.Background(), cfg.PathToken, r, []byte(body))
		return err
	}
	if err := request("first", signature); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"first", "changed", ""} {
		if err := request(key, strings.ToUpper(signature)); !errors.Is(err, ErrWebhookReplay) {
			t.Fatalf("replay accepted: %v", err)
		}
	}
}

func TestReplayClaimAtomicAndFailClosed(t *testing.T) {
	svc, _, _ := newTestSvc(t)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := svc.claimWebhook(context.Background(), "same", time.Minute)
			if err != nil {
				t.Error(err)
			}
			if ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("claimed %d times", wins.Load())
	}
	svc.ClaimReplay = func(context.Context, string, time.Duration) (bool, error) {
		return false, errors.New("database offline")
	}
	if ok, err := svc.claimWebhook(context.Background(), "new", time.Minute); ok || err == nil {
		t.Fatal("claim failed open")
	}
}

func TestBearerRequiresIdempotencyKey(t *testing.T) {
	svc, _, _ := newTestSvc(t)
	_, secrets, cfg := createWebhook(t, svc, `{"auth_modes":["bearer"]}`)
	r := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
	r.Header.Set("Authorization", "Bearer "+secrets["bearer_token"])
	if _, _, err := svc.HandleWebhook(context.Background(), cfg.PathToken, r, []byte("{}")); !errors.Is(err, ErrForbiddenWebhook) {
		t.Fatalf("missing key: %v", err)
	}
}

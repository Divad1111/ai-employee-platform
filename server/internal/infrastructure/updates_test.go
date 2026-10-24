package infrastructure

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestManifestDiscoveryDownloadAndCredentialOrigin(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	cred, err := s.SaveCredential(ctx, "api", "source", "source-token", "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	sw, err := s.SaveSoftware(ctx, Software{ID: "codex", Name: "Codex", Recipe: "codex", Binary: "codex", SourceType: "manifest", SourceURL: "https://updates.test/manifest", SourceSecretID: cred.ID})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("binary"))
	digest := hex.EncodeToString(sum[:])
	s.downloadTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := "binary"
		if req.URL.Host == "updates.test" {
			if req.Header.Get("Authorization") != "Bearer source-token" {
				t.Error("source auth missing")
			}
			b, _ := json.Marshal(map[string]any{"releases": []Release{{Version: "1.10.0", OS: "linux", Arch: "amd64", Format: "binary", URL: "https://packages.test/codex", SHA256: digest}}})
			body = string(b)
		} else if req.Header.Get("Authorization") != "" {
			t.Error("credential leaked to package host")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	releases, err := s.CheckUpdates(ctx, sw.ID, "admin")
	if err != nil || len(releases) != 1 {
		t.Fatal(releases, err)
	}
	_, f, err := s.CachedPackage(ctx, releases[0].ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	actual, _ := io.ReadAll(f)
	if string(actual) != "binary" {
		t.Fatal(string(actual))
	}
	again, err := s.CheckUpdates(ctx, sw.ID, "admin")
	if err != nil || len(again) != 1 {
		t.Fatal(again, err)
	}
}

func localUpdateFixture(t *testing.T) (*Service, Host, Release, Release, Policy) {
	t.Helper()
	s := testService(t)
	ctx := context.Background()
	h := testHost(t, s)
	sw, err := s.SaveSoftware(ctx, Software{ID: "codex", Name: "Codex", Recipe: "codex", Binary: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	r1 := testRelease(t, s, "1.9.0")
	r2 := testRelease(t, s, "1.10.0")
	inst := Installation{ID: h.ID + "-" + sw.ID, HostID: h.ID, SoftwareID: sw.ID, ReleaseID: r1.ID, Version: r1.Version, Status: "INSTALLED"}
	if err := save(ctx, s.Store, "installation", inst.ID, inst, false); err != nil {
		t.Fatal(err)
	}
	p, err := s.SavePolicy(ctx, Policy{HostID: h.ID, SoftwareID: sw.ID, Enabled: true, AutoInstall: true, IntervalMinutes: 1440, Start: "00:00", End: "00:00", Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatal(err)
	}
	return s, h, r1, r2, p
}

func TestManualSourceAutoUpdateAndPinnedVersion(t *testing.T) {
	s, _, r1, r2, p := localUpdateFixture(t)
	ctx := context.Background()
	p.PinnedReleaseID = r1.ID
	if _, err := s.SavePolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	s.reconcile(ctx)
	ops, _ := s.Store.Operations(ctx)
	if len(ops) != 0 {
		t.Fatal("locked version should not upgrade", ops)
	}
	p.PinnedReleaseID = ""
	p.LastChecked = time.Now().UTC()
	if _, err := s.SavePolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	s.reconcile(ctx)
	ops, _ = s.Store.Operations(ctx)
	if len(ops) != 1 || ops[0].Spec.ReleaseID != r2.ID || !ops[0].Spec.Automatic {
		t.Fatal(ops)
	}
}

func TestWindowEvaluationDoesNotWaitForNextSourceCheck(t *testing.T) {
	s, _, _, _, p := localUpdateFixture(t)
	ctx := context.Background()
	p.LastChecked = time.Now().UTC()
	if err := save(ctx, s.Store, "policy", p.ID, p, false); err != nil {
		t.Fatal(err)
	}
	s.reconcile(ctx)
	ops, _ := s.Store.Operations(ctx)
	if len(ops) != 1 {
		t.Fatal("local known update should deploy inside window even before next check", ops)
	}
}

func TestDisabledPolicyStopsQueuedAutomaticDeployment(t *testing.T) {
	s, _, _, _, p := localUpdateFixture(t)
	ctx := context.Background()
	s.reconcile(ctx)
	p.Enabled = false
	if _, err := s.SavePolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	o, _ := s.Store.Claim(ctx, "worker")
	if o == nil {
		t.Fatal("expected queued op")
	}
	remote := &fakeRemote{}
	s.Remote = remote
	s.execute(ctx, o)
	actual, _ := s.Store.Operation(ctx, o.ID)
	if actual.Status != "FAILED" || remote.uploads != 0 {
		t.Fatal(actual)
	}
}

func TestFailedAutomaticVersionDoesNotLoop(t *testing.T) {
	s, _, _, _, _ := localUpdateFixture(t)
	ctx := context.Background()
	s.reconcile(ctx)
	o, _ := s.Store.Claim(ctx, "worker")
	o.Status = "FAILED"
	o.Error = "health failed"
	if err := s.Store.SaveOperation(ctx, o); err != nil {
		t.Fatal(err)
	}
	s.reconcile(ctx)
	ops, _ := s.Store.Operations(ctx)
	if len(ops) != 1 {
		t.Fatal("failed update retried automatically", ops)
	}
	policies, _ := s.Policies(ctx)
	if policies[0].LastError == "" {
		t.Fatal("missing policy failure information")
	}
}

func TestMissingInstallationIsNotBootstrappedByAutomaticPolicy(t *testing.T) {
	s, h, _, _, _ := localUpdateFixture(t)
	ctx := context.Background()
	if err := s.Store.Delete(ctx, "installation", h.ID+"-codex"); err != nil {
		t.Fatal(err)
	}
	s.reconcile(ctx)
	ops, _ := s.Store.Operations(ctx)
	if len(ops) != 0 {
		t.Fatal("automatic policy bootstrapped a machine", ops)
	}
}

func TestOldPolicyProgressCannotReenableDisabledPolicy(t *testing.T) {
	s, _, _, _, old := localUpdateFixture(t)
	ctx := context.Background()
	disabled := old
	disabled.Enabled = false
	if _, err := s.SavePolicy(ctx, disabled); err != nil {
		t.Fatal(err)
	}
	old.LastError = "stale error"
	if err := s.policyProgress(ctx, old); err != nil {
		t.Fatal(err)
	}
	actual, err := load[Policy](ctx, s.Store, "policy", old.ID)
	if err != nil || actual.Enabled || actual.LastError == "stale error" || actual.Revision <= old.Revision {
		t.Fatal(actual, err)
	}
}

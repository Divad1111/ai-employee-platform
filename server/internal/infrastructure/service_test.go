package infrastructure

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/secret"
)

func testService(t *testing.T) *Service {
	t.Helper()
	vault, err := secret.NewMemoryVault()
	if err != nil {
		t.Fatal(err)
	}
	s := New(NewMemoryStore(), secret.NewManager(vault, secret.NewMemoryBindings(), nil), vault, t.TempDir())
	if err := s.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}
func testHost(t *testing.T, s *Service) Host {
	t.Helper()
	c, err := s.SaveCredential(context.Background(), "ssh", "test", `{"password":"host-password"}`, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.SaveHost(context.Background(), Host{Name: "test", Address: "127.0.0.1", SSHPort: 22, SSHUser: "ubuntu", SecretID: c.ID, HostKey: "SHA256:known", OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func testRelease(t *testing.T, s *Service, version string) Release {
	t.Helper()
	r, err := s.AddRelease(context.Background(), Release{SoftwareID: "codex", Version: version, OS: "linux", Arch: "amd64", Format: "binary"}, strings.NewReader("binary-"+version))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestManualReleaseWithoutSourceAndImmutableVersion(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	sw, err := s.SaveSoftware(ctx, Software{Name: "Tool", Recipe: "binary", Binary: "tool"})
	if err != nil {
		t.Fatal(err)
	}
	if sw.SourceURL != "" || sw.SourceType != "" {
		t.Fatal(sw)
	}
	r, err := s.AddRelease(ctx, Release{SoftwareID: sw.ID, Version: "1.0.0", OS: "linux", Arch: "amd64", Format: "binary"}, strings.NewReader("program"))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Cached || len(r.SHA256) != 64 {
		t.Fatal(r)
	}
	_, err = s.AddRelease(ctx, Release{SoftwareID: sw.ID, Version: "1.0.0", OS: "linux", Arch: "amd64", Format: "binary"}, strings.NewReader("other"))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate version must fail: %v", err)
	}
	versions, err := s.CheckUpdates(ctx, sw.ID, "admin")
	if err != nil || len(versions) != 1 {
		t.Fatal(versions, err)
	}
	_, f, err := s.CachedPackage(ctx, r.ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	content, _ := io.ReadAll(f)
	if string(content) != "program" {
		t.Fatal(string(content))
	}
}

func TestPackageChecksumMismatchDoesNotPublish(t *testing.T) {
	s := testService(t)
	_, err := s.AddRelease(context.Background(), Release{SoftwareID: "codex", Version: "1", OS: "linux", Arch: "amd64", Format: "binary", SHA256: strings.Repeat("0", 64)}, strings.NewReader("wrong"))
	if err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	releases, _ := s.Releases(context.Background())
	if len(releases) != 0 {
		t.Fatal(releases)
	}
}

func TestArchiveExtractsOnlyRegularEntrypoint(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "regular", true: "symlink"}[symlink], func(t *testing.T) {
			s := testService(t)
			var buf bytes.Buffer
			gz := gzip.NewWriter(&buf)
			tw := tar.NewWriter(gz)
			h := &tar.Header{Name: "bin/codex-x86_64", Mode: 0755, Size: 7, Typeflag: tar.TypeReg}
			if symlink {
				h.Typeflag = tar.TypeSymlink
				h.Linkname = "/etc/passwd"
				h.Size = 0
			}
			if err := tw.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
			if !symlink {
				_, _ = tw.Write([]byte("program"))
			}
			_ = tw.Close()
			_ = gz.Close()
			r, err := s.AddRelease(context.Background(), Release{SoftwareID: "codex", Version: "1", OS: "linux", Arch: "amd64", Format: "tar.gz"}, &buf)
			if err != nil {
				t.Fatal(err)
			}
			_, f, err := s.CachedPackage(context.Background(), r.ID, "admin")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			reader, err := binaryReader(f, r, "codex")
			if symlink {
				if err == nil {
					t.Fatal("symlink was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(reader)
			if err != nil || string(b) != "program" {
				t.Fatal(string(b), err)
			}
		})
	}
}

func TestQueueIdempotencyAndExclusiveHost(t *testing.T) {
	s := testService(t)
	h := testHost(t, s)
	r := testRelease(t, s, "1.0.0")
	ctx := context.Background()
	spec := Spec{Action: "install", HostID: h.ID, ReleaseID: r.ID}
	a, err := s.Queue(ctx, spec, "admin", "key")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Queue(ctx, spec, "admin", "key")
	if err != nil || a.ID != b.ID {
		t.Fatal(a, b, err)
	}
	if _, err := s.Queue(ctx, spec, "admin", "different"); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := s.Queue(ctx, Spec{Action: "inspect", HostID: h.ID, SoftwareID: "codex"}, "admin", "key"); !errors.Is(err, ErrConflict) {
		t.Fatal("same key changed payload", err)
	}
	ops, _ := s.Store.Operations(ctx)
	raw, _ := json.Marshal(ops)
	if strings.Contains(string(raw), "host-password") {
		t.Fatal("credential leaked into operations")
	}
}

func TestOnlyOneWorkerCanClaimAndRetryPreservesIdentity(t *testing.T) {
	s := testService(t)
	h := testHost(t, s)
	r := testRelease(t, s, "1")
	ctx := context.Background()
	op, err := s.Queue(ctx, Spec{Action: "install", HostID: h.ID, ReleaseID: r.ID}, "admin", "key")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	claimed := []*Operation{}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, err := s.Store.Claim(ctx, newID())
			if err != nil {
				t.Error(err)
			}
			if o != nil {
				mu.Lock()
				claimed = append(claimed, o)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(claimed) != 1 {
		t.Fatal(len(claimed))
	}
	o := claimed[0]
	o.Status = "FAILED"
	if err := s.Store.SaveOperation(ctx, o); err != nil {
		t.Fatal(err)
	}
	retry, err := s.Retry(ctx, o.ID, "admin")
	if err != nil || retry.ID != op.ID || retry.Status != "QUEUED" {
		t.Fatal(retry, err)
	}
}

func TestExpiredLeaseIsInterruptedAndCannotCommit(t *testing.T) {
	s := testService(t)
	h := testHost(t, s)
	r := testRelease(t, s, "1")
	ctx := context.Background()
	_, _ = s.Queue(ctx, Spec{Action: "install", HostID: h.ID, ReleaseID: r.ID}, "admin", "key")
	o, err := s.Store.Claim(ctx, "worker")
	if err != nil {
		t.Fatal(err)
	}
	mem := s.Store.(*MemoryStore)
	mem.mu.Lock()
	v := mem.ops[o.ID]
	v.LeaseUntil = time.Now().Add(-time.Minute)
	mem.ops[o.ID] = v
	mem.mu.Unlock()
	_, _ = s.Store.Claim(ctx, "other")
	o.Status = "SUCCESS"
	if !errors.Is(s.Store.SaveOperation(ctx, o), ErrConflict) {
		t.Fatal("stale worker committed")
	}
	actual, _ := s.Store.Operation(ctx, o.ID)
	if actual.Status != "INTERRUPTED" {
		t.Fatal(actual)
	}
}

func TestWindowAndVersionOrdering(t *testing.T) {
	cases := []struct {
		a, b  string
		newer bool
	}{{"1.10.0", "1.9.0", true}, {"1.2.0", "1.2.0-rc.1", true}, {"1.2.0-rc.2", "1.2.0-rc.10", false}, {"1.2.0+build1", "1.2.0+build2", false}, {"0.143.0", "0.99.0", true}}
	for _, c := range cases {
		if got := newer(c.a, c.b); got != c.newer {
			t.Errorf("%s > %s = %v", c.a, c.b, got)
		}
	}
	p := Policy{Start: "23:00", End: "02:00", Timezone: "Asia/Shanghai"}
	for _, c := range []struct {
		at     string
		inside bool
	}{{"2026-10-24T16:00:00Z", true}, {"2026-10-24T18:00:00Z", false}, {"2026-10-24T15:00:00Z", true}} {
		now, _ := time.Parse(time.RFC3339, c.at)
		if inWindow(p, now) != c.inside {
			t.Error(c)
		}
	}
}

func TestCredentialIsolationAndURLValidation(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	ref, _ := s.Vault.Put("feishu.system", "do-not-reveal")
	if _, err := s.RevealCredential(ctx, ref.ID, "admin"); err == nil {
		t.Fatal("system credential exposed")
	}
	if _, err := s.SaveCluster(ctx, Cluster{Name: "bad", URL: "https://localhost", SecretID: ref.ID}); err == nil {
		t.Fatal("system credential bound to cluster")
	}
	for _, raw := range []string{"http://pve:8006", "https://name:password@pve:8006", "file:///etc/passwd", "https://pve:8006?token=secret"} {
		if validateURL(raw, true) == nil {
			t.Error(raw)
		}
	}
	sw := Software{SourceURL: "https://trusted.example/manifest", SourceSecretID: "secret"}
	if packageSecret(sw, "https://other.example/file") != "" {
		t.Fatal("source credential forwarded across origin")
	}
}

type fakeRemote struct {
	scripts     []string
	uploads     int
	failInstall bool
}

func (f *fakeRemote) Connect(context.Context, Host, string) (RemoteSession, error) { return f, nil }
func (f *fakeRemote) Probe(context.Context, Host) (string, error)                  { return "SHA256:test", nil }
func (f *fakeRemote) Close() error                                                 { return nil }
func (f *fakeRemote) Upload(_ context.Context, _ string, r io.Reader) error {
	f.uploads++
	_, err := io.Copy(io.Discard, r)
	return err
}
func (f *fakeRemote) Run(_ context.Context, script string) (string, error) {
	f.scripts = append(f.scripts, script)
	switch {
	case strings.Contains(script, "mktemp -d"):
		return "/tmp/aie.1234567890\n", nil
	case strings.Contains(script, "login status"):
		return "REQUIRED", nil
	case strings.Contains(script, "dest=\""):
		if f.failInstall {
			return "health failed", errors.New("failed")
		}
		return "codex 1.0.0\n", nil
	default:
		return "", nil
	}
}

func TestInstallUpgradeAndRollbackKeepSeparateAuthState(t *testing.T) {
	s := testService(t)
	h := testHost(t, s)
	r1 := testRelease(t, s, "1.0.0")
	r2 := testRelease(t, s, "1.1.0")
	remote := &fakeRemote{}
	s.Remote = remote
	ctx := context.Background()
	run := func(action string, r Release) {
		t.Helper()
		o, err := s.Queue(ctx, Spec{Action: action, HostID: h.ID, ReleaseID: r.ID}, "admin", newID())
		if err != nil {
			t.Fatal(err)
		}
		claimed, err := s.Store.Claim(ctx, "worker")
		if err != nil {
			t.Fatal(err)
		}
		s.execute(ctx, claimed)
		done, _ := s.Store.Operation(ctx, o.ID)
		if done.Status != "SUCCESS" {
			t.Fatal(done.Error)
		}
	}
	run("install", r1)
	run("install", r2)
	inst, err := load[Installation](ctx, s.Store, "installation", h.ID+"-codex")
	if err != nil || inst.ReleaseID != r2.ID || inst.PreviousReleaseID != r1.ID || inst.AuthStatus != "REQUIRED" {
		t.Fatal(inst, err)
	}
	run("rollback", r1)
	inst, _ = load[Installation](ctx, s.Store, "installation", h.ID+"-codex")
	if inst.ReleaseID != r1.ID || inst.PreviousReleaseID != r2.ID {
		t.Fatal(inst)
	}
}

func TestBusyHostWaitsAndCanCancel(t *testing.T) {
	s := testService(t)
	h := testHost(t, s)
	h.WorkstationID = "WS-1"
	_ = save(context.Background(), s.Store, "host", h.ID, h, false)
	r := testRelease(t, s, "1")
	s.Busy = func(context.Context, string) (bool, error) { return true, nil }
	ctx := context.Background()
	o, _ := s.Queue(ctx, Spec{Action: "install", HostID: h.ID, ReleaseID: r.ID}, "admin", "key")
	claimed, _ := s.Store.Claim(ctx, "worker")
	s.execute(ctx, claimed)
	actual, _ := s.Store.Operation(ctx, o.ID)
	if actual.Status != "WAITING" {
		t.Fatal(actual)
	}
	blocked, err := s.HostMaintenance(ctx, "WS-1")
	if err != nil || !blocked {
		t.Fatal(blocked, err)
	}
	if err := s.Store.Cancel(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	blocked, _ = s.HostMaintenance(ctx, "WS-1")
	if blocked {
		t.Fatal("cancelled operation still blocks scheduler")
	}
}

func TestTamperedCacheNeverReachesRemote(t *testing.T) {
	s := testService(t)
	h := testHost(t, s)
	r := testRelease(t, s, "1")
	if err := os.WriteFile(s.artifactPath(r.ID), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	remote := &fakeRemote{}
	s.Remote = remote
	ctx := context.Background()
	o, _ := s.Queue(ctx, Spec{Action: "install", HostID: h.ID, ReleaseID: r.ID}, "admin", "key")
	claimed, _ := s.Store.Claim(ctx, "worker")
	s.execute(ctx, claimed)
	done, _ := s.Store.Operation(ctx, o.ID)
	if done.Status != "FAILED" || remote.uploads != 0 {
		t.Fatal(done, remote.uploads)
	}
}

func TestShellQuoteDoesNotInterpolateInput(t *testing.T) {
	raw := "x'; $(cat /etc/passwd)\n'"
	q := shellQuote(raw)
	if !strings.HasPrefix(q, "'") || !strings.Contains(q, "'\"'\"'") {
		t.Fatal(q)
	}
}

func TestTerminalTicketIsOneUseAndExpires(t *testing.T) {
	s := testService(t)
	h := testHost(t, s)
	ctx := context.Background()
	ticket, err := s.TerminalTicket(ctx, h.ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	s.ticketMu.Lock()
	v := s.tickets[ticket]
	v.Expires = time.Now().Add(-time.Second)
	s.tickets[ticket] = v
	s.ticketMu.Unlock()
	if _, _, err := s.OpenTerminal(ctx, ticket); err == nil {
		t.Fatal("expired ticket accepted")
	}
	if _, _, err := s.OpenTerminal(ctx, ticket); err == nil {
		t.Fatal("ticket used twice")
	}
}

type fakeAEWRemote struct{ fakeRemote }

func (f *fakeAEWRemote) Run(ctx context.Context, script string) (string, error) {
	if strings.Contains(script, "aew config show") {
		f.scripts = append(f.scripts, script)
		return "name=test endpoint=controlplane.example:19090 max_sessions=1\n", nil
	}
	return f.fakeRemote.Run(ctx, script)
}
func TestAEWHealthUsesRegisteredGRPCAddress(t *testing.T) {
	remote := &fakeAEWRemote{}
	if err := aewPing(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, script := range remote.scripts {
		if strings.Contains(script, "aew ping --grpc 'controlplane.example:19090'") {
			found = true
		}
	}
	if !found {
		t.Fatal("health check used default localhost address", remote.scripts)
	}
}

func TestFailedUpgradeKeepsPreviousInstallationRecord(t *testing.T) {
	s := testService(t)
	h := testHost(t, s)
	r1 := testRelease(t, s, "1.0.0")
	r2 := testRelease(t, s, "1.1.0")
	ctx := context.Background()
	inst := Installation{ID: h.ID + "-codex", HostID: h.ID, SoftwareID: "codex", ReleaseID: r1.ID, Version: r1.Version, Status: "INSTALLED"}
	_ = save(ctx, s.Store, "installation", inst.ID, inst, false)
	remote := &fakeRemote{failInstall: true}
	s.Remote = remote
	o, _ := s.Queue(ctx, Spec{Action: "install", HostID: h.ID, ReleaseID: r2.ID}, "admin", "key")
	claimed, _ := s.Store.Claim(ctx, "worker")
	s.execute(ctx, claimed)
	actual, _ := s.Store.Operation(ctx, o.ID)
	if actual.Status != "FAILED" {
		t.Fatal(actual)
	}
	current, _ := load[Installation](ctx, s.Store, "installation", inst.ID)
	if current.ReleaseID != r1.ID || current.Version != r1.Version {
		t.Fatal(current)
	}
	if !strings.Contains(strings.Join(remote.scripts, "\n"), "trap restore EXIT") {
		t.Fatal("missing automatic restoration")
	}
}

package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	input "github.com/ai-employee-platform/gen/jobinput"
	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/jobinput"
)

func TestJobInputDownloadRequiresAssignedNodeAndManifest(t *testing.T) {
	ctx := context.Background()
	store := &jobinput.Store{Root: t.TempDir()}
	file, err := store.Put("需求.txt", strings.NewReader("input contents"))
	if err != nil {
		t.Fatal(err)
	}
	jobs := job.NewService(job.NewMemoryStore(), audit.NewMemory(), eventbus.New(10))
	j, _, err := jobs.Create(ctx, job.CreateInput{EmployeeID: "EMP", WorkstationID: "WS-allowed", IdempotencyKey: "input", InputAttachments: []input.File{file}}, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := certca.NewDevAuthority()
	key, cert := issueWSCert(t, ca, "WS-allowed")
	otherKey, otherCert := issueWSCert(t, ca, "WS-other")
	h := api.NewRouter(api.Deps{Jobs: jobs, CA: ca, InputFiles: store})
	for _, tc := range []struct {
		ws, hash string
		signed   bool
		status   int
	}{
		{"WS-allowed", file.SHA256, true, 200},
		{"WS-other", file.SHA256, true, 404},
		{"WS-allowed", strings.Repeat("0", 64), true, 404},
		{"WS-allowed", file.SHA256, false, 401},
	} {
		r := httptest.NewRequest(http.MethodGet, "/api/integrations/workstation/jobs/"+j.ID+"/inputs/"+tc.hash, nil)
		ts := time.Now().UTC().Format(time.RFC3339)
		k, c := key, cert
		if tc.ws == "WS-other" {
			k, c = otherKey, otherCert
		}
		r.Header.Set("X-AIE-Workstation-Id", tc.ws)
		r.Header.Set("X-AIE-Timestamp", ts)
		r.Header.Set("X-AIE-Client-Cert", strings.ReplaceAll(c, "\n", `\n`))
		if tc.signed {
			r.Header.Set("X-AIE-Signature", signEC(t, k, input.DownloadPayload(tc.ws, ts, j.ID, tc.hash)))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("ws=%s expected %d got %d: %s", tc.ws, tc.status, w.Code, w.Body.String())
		}
		if w.Code == 200 && w.Body.String() != "input contents" {
			t.Fatal("wrong file bytes")
		}
	}
}

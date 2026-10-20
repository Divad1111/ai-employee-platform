package artifactlocal

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	input "github.com/ai-employee-platform/gen/jobinput"
	"github.com/ai-employee-platform/workstation/internal/identity"
)

func TestDownloadInputsChecksSignatureAndContent(t *testing.T) {
	bundle, key, _ := identity.Generate()
	data := []byte("user supplied file")
	sum := sha256.Sum256(data)
	file := input.File{SHA256: hex.EncodeToString(sum[:]), Name: "需求.pdf", MIMEType: "application/pdf", Size: int64(len(data))}
	var corrupt atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sig, _ := hex.DecodeString(r.Header.Get("X-AIE-Signature"))
		digest := sha256.Sum256([]byte(input.DownloadPayload(bundle.WorkstationID, r.Header.Get("X-AIE-Timestamp"), "JOB-1", file.SHA256)))
		if !ecdsa.VerifyASN1(&key.PublicKey, digest[:], sig) {
			t.Error("download request has no valid identity proof")
			w.WriteHeader(401)
			return
		}
		if corrupt.Load() {
			w.Write([]byte("corrupted"))
			return
		}
		w.Write(data)
	}))
	defer server.Close()
	u := &Uploader{HTTPBase: server.URL, Bundle: bundle}
	root := t.TempDir()
	for i := 0; i < 2; i++ { // replay replaces a previous local file safely on Windows as well.
		files, err := u.DownloadInputs(context.Background(), "JOB-1", root, []input.File{file})
		if err != nil || len(files) != 1 {
			t.Fatalf("download failed: %v", err)
		}
		local, err := os.ReadFile(files[0].Path)
		if err != nil || string(local) != string(data) {
			t.Fatal("download changed file bytes")
		}
		rel, _ := filepath.Rel(root, files[0].Path)
		if strings.HasPrefix(rel, "..") {
			t.Fatal("download escaped workspace")
		}
	}
	corrupt.Store(true)
	if _, err := u.DownloadInputs(context.Background(), "JOB-1", root, []input.File{file}); err == nil {
		t.Fatal("corrupt response accepted")
	}
}

func TestDownloadInputsDoesNotForwardIdentityToRedirect(t *testing.T) {
	bundle, _, _ := identity.Generate()
	var requests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer server.Close()
	u := &Uploader{HTTPBase: server.URL, Bundle: bundle}
	_, err := u.DownloadInputs(context.Background(), "JOB-1", t.TempDir(), []input.File{{SHA256: strings.Repeat("0", 64), Name: "a.txt", Size: 1}})
	if err == nil || requests.Load() != 0 {
		t.Fatal("signed identity followed redirect")
	}
}

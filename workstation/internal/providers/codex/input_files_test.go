package codex

import (
	"context"
	"github.com/ai-employee-platform/workstation/internal/acp"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexTurnReceivesNativeImageAndDoesNotReuseIt(t *testing.T) {
	s := newAppServerSession("files", "unused", t.TempDir(), "test-model")
	s.ready = true
	peer := &mcpRPCPeer{s: s}
	s.stdin = peer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	textPath := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(textPath, []byte("测试文件"), 0o600); err != nil {
		t.Fatal(err)
	}
	files := []acp.InputFile{{Path: "C:/workspace/image.png", MIMEType: "image/png"}, {Path: "C:/workspace/report.pdf", MIMEType: "application/pdf"}, {Path: textPath, Name: "test.txt", MIMEType: "text/plain"}}
	imageCtx := acp.WithInputFiles(ctx, files)
	if _, err := s.Send(imageCtx, []byte(acp.PromptWithFiles("inspect files", files))); err != nil {
		t.Fatal(err)
	}
	blocks := peer.requests[1]["params"].(map[string]any)["input"].([]any)
	if len(blocks) != 2 || blocks[1].(map[string]any)["type"] != "localImage" || blocks[1].(map[string]any)["path"] != "C:/workspace/image.png" {
		t.Fatal("image did not reach turn/start")
	}
	if !strings.Contains(blocks[0].(map[string]any)["text"].(string), textPath) {
		t.Fatal("text file path did not reach turn/start")
	}
	if _, err := s.Send(ctx, []byte("next task")); err != nil {
		t.Fatal(err)
	}
	blocks = peer.requests[2]["params"].(map[string]any)["input"].([]any)
	if len(blocks) != 1 || blocks[0].(map[string]any)["text"] != "next task" {
		t.Fatal("next task inherited previous file inputs")
	}
}

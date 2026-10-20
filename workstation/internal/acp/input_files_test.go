package acp

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCursorSendsNativeImageInPrompt(t *testing.T) {
	binary, _ := os.Executable()
	root := t.TempDir()
	image := filepath.Join(root, "image.png")
	if err := os.WriteFile(image, []byte("image bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	textPath := filepath.Join(root, "test.txt")
	if err := os.WriteFile(textPath, []byte("测试文件"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStdioSession("input-files", binary, []string{"-test.run=^TestACPInputFilesPeer$"}, root)
	s.SetEnv([]string{"AIE_TEST_ACP_INPUT_FILES_PEER=1"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer s.Stop(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	files := []InputFile{{Path: image, MIMEType: "image/png"}, {Path: textPath, Name: "test.txt", MIMEType: "text/plain"}}
	if _, err := s.Send(WithInputFiles(ctx, files), []byte(PromptWithFiles("inspect image and file", files))); err != nil {
		t.Fatal(err)
	}
}

func TestACPInputFilesPeer(t *testing.T) {
	if os.Getenv("AIE_TEST_ACP_INPUT_FILES_PEER") != "1" {
		return
	}
	scanner, encoder := bufio.NewScanner(os.Stdin), json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			os.Exit(2)
		}
		result := map[string]any{}
		if req.Method == "session/new" {
			result["sessionId"] = "input-session"
		}
		if req.Method == "session/prompt" {
			blocks, ok := req.Params["prompt"].([]any)
			valid := ok && len(blocks) == 2
			if valid {
				valid = strings.Contains(blocks[0].(map[string]any)["text"].(string), "test.txt")
				image := blocks[1].(map[string]any)
				data, _ := base64.StdEncoding.DecodeString(image["data"].(string))
				valid = valid && image["type"] == "image" && image["mimeType"] == "image/png" && string(data) == "image bytes"
			}
			if !valid {
				encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32602, "message": "native image missing"}})
				os.Exit(1)
			}
		}
		encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
	os.Exit(0)
}

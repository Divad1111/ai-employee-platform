package cursor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ai-employee-platform/workstation/internal/providers"
)

// ListModels 从 `agent acp` 的 session configOptions 读取可设置的模型。
// `agent models` 的 id（如 gemini-3.8-flash-medium）不能传给 ACP，传了会静默停在 Auto。
func (p *Provider) ListModels(ctx context.Context) ([]providers.Model, error) {
	info, err := p.Detect(ctx)
	if err != nil || info == nil || info.Path == "" || isFakePath(info.Path) || isGUICursorBinary(info.Path) {
		return defaultCursorModels(), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	models, err := listACPModels(ctx, info.Path)
	if err == nil && len(models) > 0 {
		return models, nil
	}
	// 降级尝试从命令行 `agent models` 读取
	if fb, ferr := listFallbackModels(ctx, info.Path); ferr == nil && len(fb) > 0 {
		return fb, nil
	}
	return defaultCursorModels(), nil
}

func listFallbackModels(ctx context.Context, bin string) ([]providers.Model, error) {
	cmd := exec.CommandContext(ctx, bin, "models")
	if userHome := findUserHome(); userHome != "" {
		cmd.Env = append(os.Environ(),
			"USERPROFILE="+userHome,
			"HOME="+userHome,
			"LOCALAPPDATA="+filepath.Join(userHome, "AppData", "Local"),
			"APPDATA="+filepath.Join(userHome, "AppData", "Roaming"),
		)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	return ParseModelLines(string(out)), nil
}

func defaultCursorModels() []providers.Model {
	return []providers.Model{
		{ID: "default[]", Label: "Auto"},
		{ID: "claude-sonnet-4-5", Label: "Claude Sonnet 4.5"},
		{ID: "claude-opus-4-5", Label: "Claude Opus 4.5"},
		{ID: "gpt-5", Label: "GPT-5"},
		{ID: "composer-2.5", Label: "Composer 2.5"},
		{ID: "gemini-3-flash", Label: "Gemini 3 Flash"},
	}
}

func listACPModels(ctx context.Context, bin string) ([]providers.Model, error) {
	cmd := exec.CommandContext(ctx, bin, "acp")
	if userHome := findUserHome(); userHome != "" {
		cmd.Env = append(os.Environ(),
			"USERPROFILE="+userHome,
			"HOME="+userHome,
			"LOCALAPPDATA="+filepath.Join(userHome, "AppData", "Local"),
			"APPDATA="+filepath.Join(userHome, "AppData", "Roaming"),
		)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	write := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		b = append(b, '\n')
		_, err = stdin.Write(b)
		return err
	}
	if err := write(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion":    1,
			"clientCapabilities": map[string]any{},
			"clientInfo":         map[string]any{"name": "aew", "version": "0"},
		},
	}); err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	if _, err := waitACP(ctx, sc, 1); err != nil {
		return nil, err
	}
	if err := write(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return nil, err
	}
	tempDir := os.TempDir()
	if tempDir == "" {
		tempDir = "."
	}
	if err := write(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "session/new",
		"params": map[string]any{"cwd": tempDir, "mcpServers": []any{}},
	}); err != nil {
		return nil, err
	}
	raw, err := waitACP(ctx, sc, 2)
	if err != nil {
		return nil, err
	}
	return modelsFromSessionResult(raw)
}

func waitACP(ctx context.Context, sc *bufio.Scanner, id int) (json.RawMessage, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !sc.Scan() {
			if err := sc.Err(); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("Cursor ACP 提前结束")
		}
		var msg struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) != nil || msg.ID != id {
			continue
		}
		if msg.Error != nil && msg.Error.Message != "" {
			return nil, fmt.Errorf("%s", msg.Error.Message)
		}
		return msg.Result, nil
	}
}

// modelsFromSessionResult 取出 category=model 的可选项。这些 value 才能 session/set_config_option。
func modelsFromSessionResult(raw json.RawMessage) ([]providers.Model, error) {
	var body struct {
		ConfigOptions []struct {
			ID       string `json:"id"`
			Category string `json:"category"`
			Options  []struct {
				Value string `json:"value"`
				Name  string `json:"name"`
			} `json:"options"`
		} `json:"configOptions"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	var out []providers.Model
	for _, opt := range body.ConfigOptions {
		if opt.Category != "model" && opt.ID != "model" {
			continue
		}
		for _, item := range opt.Options {
			if item.Value == "" {
				continue
			}
			label := item.Name
			if label == "" {
				label = item.Value
			}
			if effort := effortOf(item.Value); effort != "" && !strings.Contains(strings.ToLower(label), effort) {
				label = label + " · " + effort
			}
			out = append(out, providers.Model{ID: item.Value, Label: label})
		}
	}
	return out, nil
}

func effortOf(value string) string {
	start := strings.Index(value, "[")
	end := strings.Index(value, "]")
	if start < 0 || end <= start {
		return ""
	}
	for _, part := range strings.Split(value[start+1:end], ",") {
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if strings.Contains(key, "effort") || strings.Contains(key, "reasoning") {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

func isFakePath(path string) bool {
	base := strings.ToLower(path)
	return strings.Contains(base, "fake")
}

// ParseModelLines 解析 `id - 显示名` 行。标题和空行跳过。
func ParseModelLines(text string) []providers.Model {
	var out []providers.Model
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		id, label, ok := strings.Cut(line, " - ")
		if !ok {
			continue
		}
		id = strings.TrimSpace(id)
		label = strings.TrimSpace(label)
		if id == "" || strings.Contains(id, " ") {
			continue
		}
		if label == "" {
			label = id
		}
		out = append(out, providers.Model{ID: id, Label: label})
	}
	return out
}

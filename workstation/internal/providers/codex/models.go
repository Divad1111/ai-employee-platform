package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ai-employee-platform/workstation/internal/providers"
)

// ListModels 通过 `codex app-server` 的 model/list 读取当前账号模型。
func (p *Provider) ListModels(ctx context.Context) ([]providers.Model, error) {
	info, err := p.Detect(ctx)
	if err != nil || info == nil || info.Path == "" || strings.Contains(strings.ToLower(info.Path), "fake") {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, info.Path, "app-server", "--listen", "stdio://")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = nil
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
		"id": 1, "method": "initialize",
		"params": map[string]any{
			"clientInfo":   map[string]any{"name": "aew", "version": "0"},
			"capabilities": map[string]any{},
		},
	}); err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	if _, err := waitRPC(ctx, sc, 1); err != nil {
		return nil, err
	}
	if err := write(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return nil, err
	}
	if err := write(map[string]any{"id": 2, "method": "model/list", "params": map[string]any{}}); err != nil {
		return nil, err
	}
	raw, err := waitRPC(ctx, sc, 2)
	if err != nil {
		return nil, err
	}
	return parseCodexModelList(raw)
}

func waitRPC(ctx context.Context, sc *bufio.Scanner, id int) (json.RawMessage, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !sc.Scan() {
			if err := sc.Err(); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("codex model/list 提前结束")
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

func parseCodexModelList(raw json.RawMessage) ([]providers.Model, error) {
	var body struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			Hidden      bool   `json:"hidden"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	var out []providers.Model
	for _, item := range body.Data {
		if item.Hidden || item.ID == "" {
			continue
		}
		label := item.DisplayName
		if label == "" {
			label = item.ID
		}
		out = append(out, providers.Model{ID: item.ID, Label: label})
	}
	return out, nil
}

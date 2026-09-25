package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ai-employee-platform/workstation/internal/acp"
)

// appServerSession 通过 `codex app-server`（JSON-RPC over stdio）跑一轮对话。
// 本机 Codex CLI（ChatGPT.app 内置）没有 `codex acp` 子命令。
type appServerSession struct {
	id        string
	bin       string
	workspace string
	model     string
	mcp       []any

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	events chan acp.Event

	mu         sync.Mutex
	ready      bool
	nextID     int
	pending    map[int]chan rpcMsg
	threadID   string
	reply      strings.Builder
	turnErr    string
	turnDone   chan struct{}
	activeTurn string
	inTok      int64
	outTok     int64
}

type rpcMsg struct {
	ID     int             `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func newAppServerSession(id, bin, workspace, model string) *appServerSession {
	return &appServerSession{
		id: id, bin: bin, workspace: workspace, model: model,
		events:  make(chan acp.Event, 32),
		pending: map[int]chan rpcMsg{},
	}
}

func (s *appServerSession) SetMCPServers(servers []any) { s.mcp = servers }

func (s *appServerSession) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready
}

func (s *appServerSession) Events() <-chan acp.Event { return s.events }

func (s *appServerSession) LastUsage() (int64, int64, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inTok, s.outTok, "codex", "agent"
}

func (s *appServerSession) Start(ctx context.Context) error {
	_ = ctx
	cmd := exec.Command(s.bin, "app-server", "--listen", "stdio://")
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 Codex app-server 失败: %w", err)
	}
	s.cmd = cmd
	s.stdin = in
	go s.read(out)

	if _, err := s.request(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "aew", "version": "0"},
		"capabilities": map[string]any{},
	}); err != nil {
		_ = s.Stop(context.Background())
		return fmt.Errorf("Codex 握手失败: %w", err)
	}
	if err := s.notify("initialized", map[string]any{}); err != nil {
		_ = s.Stop(context.Background())
		return err
	}
	s.mu.Lock()
	s.ready = true
	s.mu.Unlock()
	return nil
}

func (s *appServerSession) Send(ctx context.Context, input []byte) (string, error) {
	if !s.Ready() {
		return "", acp.ErrNotStarted
	}
	s.mu.Lock()
	if s.threadID == "" {
		s.mu.Unlock()
		params := map[string]any{
			"cwd":            s.workspace,
			"approvalPolicy": "never",
			"sandbox":        "workspace-write",
		}
		if s.model != "" {
			params["model"] = s.model
		}
		res, err := s.request(ctx, "thread/start", params)
		if err != nil {
			return "", err
		}
		var body struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		_ = json.Unmarshal(res, &body)
		if body.Thread.ID == "" {
			return "", errors.New("Codex thread/start 没有返回 thread.id")
		}
		s.mu.Lock()
		s.threadID = body.Thread.ID
	}
	tid := s.threadID
	s.reply.Reset()
	s.turnErr = ""
	s.turnDone = make(chan struct{})
	s.mu.Unlock()

	res, err := s.request(ctx, "turn/start", map[string]any{
		"threadId": tid,
		"input":    []map[string]any{{"type": "text", "text": string(input)}},
	})
	if err != nil {
		return "", err
	}
	var started struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(res, &started)
	s.mu.Lock()
	s.activeTurn = started.Turn.ID
	done := s.turnDone
	s.mu.Unlock()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-done:
	}
	s.mu.Lock()
	reply := s.reply.String()
	turnErr := s.turnErr
	s.mu.Unlock()
	if turnErr != "" {
		return reply, errors.New(turnErr)
	}
	return reply, nil
}

func (s *appServerSession) Stop(_ context.Context) error {
	s.mu.Lock()
	s.ready = false
	in := s.stdin
	cmd := s.cmd
	s.mu.Unlock()
	if in != nil {
		_ = in.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	return nil
}

func (s *appServerSession) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	ch := make(chan rpcMsg, 1)
	s.pending[id] = ch
	s.mu.Unlock()
	if err := s.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg := <-ch:
		if msg.Error != nil && msg.Error.Message != "" {
			return nil, errors.New(msg.Error.Message)
		}
		return msg.Result, nil
	case <-time.After(60 * time.Second):
		if method == "initialize" || method == "thread/start" {
			return nil, fmt.Errorf("%s 超时", method)
		}
		// turn/start 只负责发出，完成由 turn/completed 通知。
		return nil, fmt.Errorf("%s 超时", method)
	}
}

func (s *appServerSession) notify(method string, params any) error {
	return s.write(map[string]any{"method": method, "params": params})
}

func (s *appServerSession) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	s.mu.Lock()
	in := s.stdin
	s.mu.Unlock()
	if in == nil {
		return errors.New("Codex 进程未启动")
	}
	_, err = in.Write(b)
	return err
}

func (s *appServerSession) read(out io.Reader) {
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var msg rpcMsg
		if json.Unmarshal(sc.Bytes(), &msg) != nil {
			continue
		}
		if msg.ID != 0 && msg.Method == "" {
			s.mu.Lock()
			ch := s.pending[msg.ID]
			delete(s.pending, msg.ID)
			s.mu.Unlock()
			if ch != nil {
				ch <- msg
			}
			continue
		}
		s.onNotify(msg)
	}
}

func (s *appServerSession) onNotify(msg rpcMsg) {
	switch msg.Method {
	case "item/completed":
		var p struct {
			Item struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"item"`
		}
		if json.Unmarshal(msg.Params, &p) != nil {
			return
		}
		if p.Item.Type != "agentMessage" && p.Item.Type != "assistantMessage" {
			return
		}
		text := p.Item.Text
		if text == "" {
			for _, c := range p.Item.Content {
				text += c.Text
			}
		}
		s.mu.Lock()
		s.reply.WriteString(text)
		s.mu.Unlock()
	case "turn/completed":
		var p struct {
			Turn struct {
				Status string `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"turn"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		s.mu.Lock()
		if p.Turn.Error != nil && p.Turn.Error.Message != "" {
			s.turnErr = p.Turn.Error.Message
		} else if p.Turn.Status == "failed" {
			s.turnErr = "Codex 回合失败"
		}
		if s.turnDone != nil {
			select {
			case <-s.turnDone:
			default:
				close(s.turnDone)
			}
		}
		s.mu.Unlock()
	case "thread/tokenUsage/updated":
		var p struct {
			InputTokens  int64 `json:"inputTokens"`
			OutputTokens int64 `json:"outputTokens"`
		}
		if json.Unmarshal(msg.Params, &p) == nil {
			s.mu.Lock()
			if p.InputTokens > 0 {
				s.inTok = p.InputTokens
			}
			if p.OutputTokens > 0 {
				s.outTok = p.OutputTokens
			}
			s.mu.Unlock()
		}
	}
}

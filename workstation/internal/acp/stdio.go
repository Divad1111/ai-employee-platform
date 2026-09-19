package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// StdioSession 通过 stdio 连接真实 ACP Server（如 `agent acp`）。
// 传输：newline-delimited JSON-RPC 2.0；不启动 Cursor GUI。
type StdioSession struct {
	id      string
	binary  string
	args    []string
	workDir string
	env     []string

	mu           sync.Mutex
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	ready        bool
	stopped      bool
	events       chan Event
	nextID       atomic.Int64
	pending      map[int64]chan rpcResult
	cancel       context.CancelFunc
	acpSessionID string
	collecting   bool
	replyBuf     strings.Builder
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

// NewStdioSession 创建 stdio ACP 会话；binary 应为 Cursor CLI `agent`，args 通常为 ["acp"]。
func NewStdioSession(sessionID, binary string, args []string, workDir string) *StdioSession {
	if len(args) == 0 {
		args = []string{"acp"}
	}
	return &StdioSession{
		id:      sessionID,
		binary:  binary,
		args:    append([]string{}, args...),
		workDir: workDir,
		events:  make(chan Event, 64),
		pending: map[int64]chan rpcResult{},
	}
}

// SetEnv 追加环境变量（如 CURSOR_API_KEY）。
func (s *StdioSession) SetEnv(env []string) { s.env = append([]string{}, env...) }

func (s *StdioSession) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.ready {
		s.mu.Unlock()
		return nil
	}
	if s.stopped {
		s.mu.Unlock()
		return ErrNotStarted
	}
	if s.binary == "" {
		s.mu.Unlock()
		return fmt.Errorf("%w: 未配置 agent 二进制", ErrHandshake)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	cmd := exec.CommandContext(runCtx, s.binary, s.args...)
	if s.workDir != "" {
		cmd.Dir = s.workDir
	}
	cmd.Env = append(os.Environ(), s.env...)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		s.mu.Unlock()
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		s.mu.Unlock()
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		cancel()
		s.mu.Unlock()
		return fmt.Errorf("启动 ACP Server 失败（请确认已安装 Cursor CLI `agent`，而非 GUI Cursor.app）: %w", err)
	}
	s.cmd = cmd
	s.stdin = stdin
	s.mu.Unlock()

	go s.readLoop(stdout)

	// 官方最小握手：initialize → authenticate → session/new
	if _, err := s.call(ctx, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": false, "writeTextFile": false},
			"terminal": false,
		},
		"clientInfo": map[string]any{"name": "aie-workstation", "version": "0.1.0"},
	}); err != nil {
		_ = s.Stop(ctx)
		return fmt.Errorf("%w: initialize: %v", ErrHandshake, err)
	}
	if _, err := s.call(ctx, "authenticate", map[string]any{"methodId": "cursor_login"}); err != nil {
		// 若本机已 agent login，部分版本仍要求该方法；失败则继续尝试 session/new
		select {
		case s.events <- Event{Type: "auth_warn", Payload: []byte(err.Error())}:
		default:
		}
	}
	cwd := s.workDir
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	raw, err := s.call(ctx, "session/new", map[string]any{
		"cwd":        cwd,
		"mcpServers": []any{},
	})
	if err != nil {
		_ = s.Stop(ctx)
		return fmt.Errorf("%w: session/new: %v", ErrHandshake, err)
	}
	var newRes struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(raw, &newRes)
	s.mu.Lock()
	if newRes.SessionID != "" {
		s.acpSessionID = newRes.SessionID
	} else {
		s.acpSessionID = s.id
	}
	s.ready = true
	s.mu.Unlock()
	select {
	case s.events <- Event{Type: "ready", Payload: []byte(s.id)}:
	default:
	}
	return nil
}

func (s *StdioSession) Send(ctx context.Context, input []byte) (string, error) {
	s.mu.Lock()
	ready := s.ready && !s.stopped
	sid := s.acpSessionID
	s.replyBuf.Reset()
	s.collecting = true
	s.mu.Unlock()
	if !ready {
		return "", ErrNotStarted
	}
	if sid == "" {
		sid = s.id
	}
	defer func() {
		s.mu.Lock()
		s.collecting = false
		s.mu.Unlock()
	}()

	_, err := s.call(ctx, "session/prompt", map[string]any{
		"sessionId": sid,
		"prompt":    []map[string]any{{"type": "text", "text": string(input)}},
	})
	// 短暂等待尾部 chunk
	time.Sleep(150 * time.Millisecond)

	s.mu.Lock()
	reply := s.replyBuf.String()
	s.mu.Unlock()
	if err != nil {
		return reply, err
	}
	if strings.TrimSpace(reply) == "" {
		reply = "(agent 未返回文本内容)"
	}
	return reply, nil
}

func (s *StdioSession) appendReply(text string) {
	if text == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.collecting {
		s.replyBuf.WriteString(text)
	}
}

// extractAgentTextFromUpdate 解析 session/update params。
func extractAgentTextFromUpdate(params json.RawMessage) string {
	var u struct {
		Update struct {
			SessionUpdate string `json:"sessionUpdate"`
			Content       struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Text string `json:"text"`
		} `json:"update"`
	}
	if err := json.Unmarshal(params, &u); err != nil {
		return ""
	}
	switch u.Update.SessionUpdate {
	case "agent_message_chunk", "agent_thought_chunk", "message_chunk":
		if u.Update.Content.Text != "" {
			return u.Update.Content.Text
		}
		return u.Update.Text
	}
	return ""
}

func (s *StdioSession) Events() <-chan Event { return s.events }

func (s *StdioSession) Stop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	s.ready = false
	if s.cancel != nil {
		s.cancel()
	}
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
	}
	for id, ch := range s.pending {
		ch <- rpcResult{err: errors.New("ACP 会话已停止")}
		delete(s.pending, id)
	}
	return nil
}

func (s *StdioSession) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready && !s.stopped
}

func (s *StdioSession) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := s.nextID.Add(1)
	ch := make(chan rpcResult, 1)
	s.mu.Lock()
	if s.stopped || s.stdin == nil {
		s.mu.Unlock()
		return nil, ErrNotStarted
	}
	s.pending[id] = ch
	msg, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	msg = append(msg, '\n')
	_, err := s.stdin.Write(msg)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return nil, ctx.Err()
	case res := <-ch:
		return res.result, res.err
	}
}

func (s *StdioSession) readLoop(stdout io.Reader) {
	sc := bufio.NewScanner(stdout)
	// ACP 流式块可能较大
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      *int64          `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
			Result  json.RawMessage `json:"result"`
			Error   *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		// 响应
		if msg.ID != nil && msg.Method == "" {
			s.mu.Lock()
			ch, ok := s.pending[*msg.ID]
			if ok {
				delete(s.pending, *msg.ID)
			}
			s.mu.Unlock()
			if ok {
				if msg.Error != nil {
					ch <- rpcResult{err: fmt.Errorf("rpc %d: %s", msg.Error.Code, msg.Error.Message)}
				} else {
					ch <- rpcResult{result: msg.Result}
				}
			}
			continue
		}
		// 服务端请求/通知
		if msg.Method != "" {
			if msg.Method == "session/update" {
				s.appendReply(extractAgentTextFromUpdate(msg.Params))
			}
			payload, _ := json.Marshal(map[string]any{"method": msg.Method, "params": msg.Params})
			select {
			case s.events <- Event{Type: msg.Method, Payload: payload}:
			default:
			}
			// 权限请求：默认 allow-once，避免无头工具调用卡死
			if msg.ID != nil && msg.Method == "session/request_permission" {
				_ = s.respond(*msg.ID, map[string]any{
					"outcome": map[string]any{"outcome": "selected", "optionId": "allow-once"},
				})
			}
		}
	}
}

func (s *StdioSession) respond(id int64, result any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stdin == nil {
		return ErrNotStarted
	}
	msg, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	msg = append(msg, '\n')
	_, err := s.stdin.Write(msg)
	return err
}

// StdioClient 按会话创建 StdioSession。
type StdioClient struct {
	Binary  string
	Args    []string
	WorkDir string
	Env     []string
}

func (c *StdioClient) Open(sessionID string) Session {
	s := NewStdioSession(sessionID, c.Binary, c.Args, c.WorkDir)
	if len(c.Env) > 0 {
		s.SetEnv(c.Env)
	}
	return s
}

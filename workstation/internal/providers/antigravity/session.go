package antigravity

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ai-employee-platform/workstation/internal/runtimeenv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ai-employee-platform/workstation/internal/acp"
)

// TranscriptStep 对应 transcript.jsonl 中单步记录。
type TranscriptStep struct {
	StepIndex int               `json:"step_index"`
	Source    string            `json:"source"`
	Type      string            `json:"type"`
	Status    string            `json:"status"`
	Content   string            `json:"content"`
	Thinking  string            `json:"thinking"`
	ToolCalls []json.RawMessage `json:"tool_calls"`
}

// RealSession 通过 Antigravity 本地 agentapi 与 Agent 交互。
type RealSession struct {
	sessionID      string
	workspacePath  string
	agentCmd       string
	agentArgs      []string
	conversationID string
	brainDir       string
	mu             sync.Mutex
	ready          bool
	stopped        bool
	events         chan acp.Event
	inquiryHandler acp.InquiryHandler
}

// NewRealSession 创建真实 Antigravity Session。
func NewRealSession(sessionID, agentCmd string, agentArgs []string, workspacePath string) *RealSession {
	home, _ := os.UserHomeDir()
	brainDir := os.Getenv("ANTIGRAVITY_BRAIN_DIR")
	if brainDir == "" {
		brainDir = filepath.Join(home, ".gemini", "antigravity", "brain")
	}
	return &RealSession{
		sessionID:     sessionID,
		workspacePath: workspacePath,
		agentCmd:      agentCmd,
		agentArgs:     agentArgs,
		brainDir:      brainDir,
		events:        make(chan acp.Event, 16),
	}
}

func (s *RealSession) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return errors.New("session 已停止")
	}
	s.ready = true
	select {
	case s.events <- acp.Event{Type: "ready", Payload: []byte(s.sessionID)}:
	default:
	}
	return nil
}

func (s *RealSession) SetInquiryHandler(h acp.InquiryHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inquiryHandler = h
}

func (s *RealSession) Send(ctx context.Context, input []byte) (string, error) {
	s.mu.Lock()
	if !s.ready || s.stopped {
		s.mu.Unlock()
		return "", acp.ErrNotStarted
	}
	handler := s.inquiryHandler
	s.mu.Unlock()
	const questionPrefix = "【玩家选择】"
	instruction := acp.ResultInstructions + " 如需玩家选择，请先仅返回【玩家选择】及 JSON 对象，包含 question 字符串和 options 字符串数组，等待答复再继续。"
	prompt := instruction + "\n\n" + string(input)
	var reply string
	for {
		var err error
		reply, err = s.sendTurn(ctx, prompt)
		if err != nil {
			return "", err
		}
		raw, asking := strings.CutPrefix(strings.TrimSpace(reply), questionPrefix)
		if !asking {
			break
		}
		var q struct {
			Question string
			Options  []string
		}
		if json.Unmarshal([]byte(raw), &q) != nil || q.Question == "" || handler == nil {
			return "", errors.New("无法向玩家提交确认请求")
		}
		inq := acp.Inquiry{SessionID: s.sessionID, Method: "user_choice", Message: q.Question, AllowText: len(q.Options) == 0}
		for i, name := range q.Options {
			inq.Options = append(inq.Options, acp.InquiryOption{OptionID: fmt.Sprintf("choice-%d", i+1), Name: name})
		}
		choice, err := handler(ctx, inq)
		if err != nil {
			return "", err
		}
		answer := ""
		for _, o := range inq.Options {
			if o.OptionID == choice {
				answer = o.Name
				break
			}
		}
		if inq.AllowText && strings.HasPrefix(choice, "text:") {
			answer = strings.TrimPrefix(choice, "text:")
		}
		if answer == "" {
			return "", errors.New("未收到有效选择")
		}
		prompt = instruction + "\n玩家对上一条询问的回答：" + answer
	}
	reply, evidence, err := acp.ReadResult(reply)
	if err != nil {
		return reply, err
	}
	select {
	case s.events <- acp.Event{Type: "agent_activity", Payload: []byte("完成核验：" + evidence)}:
	default:
	}
	if reply != "" {
		select {
		case s.events <- acp.Event{Type: "assistant_delta", Payload: []byte(reply)}:
		default:
		}
	}

	select {
	case s.events <- acp.Event{Type: "ack", Payload: []byte(reply)}:
	default:
	}
	return reply, nil
}

func (s *RealSession) sendTurn(ctx context.Context, prompt string) (string, error) {
	var startLineCount int

	if s.conversationID == "" {
		// 首次调用：new-conversation
		title := fmt.Sprintf("Workstation-%s", s.sessionID)
		args := append([]string{}, s.agentArgs...)
		args = append(args, "new-conversation", "--title="+title, prompt)

		cmd := buildCommand(ctx, s.agentCmd, args, s.workspacePath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("调用 agentapi new-conversation 失败: %w, 输出: %s", err, string(out))
		}

		convID, err := parseConversationID(out)
		if err != nil {
			return "", fmt.Errorf("解析 conversationId 失败: %w (原始输出: %s)", err, string(out))
		}
		s.mu.Lock()
		s.conversationID = convID
		s.mu.Unlock()
		startLineCount = 0
	} else {
		// 后续调用：获取当前 transcript 行数后 send-message
		transcriptPath := s.transcriptPath(s.conversationID)
		startLineCount = countLines(transcriptPath)

		args := append([]string{}, s.agentArgs...)
		args = append(args, "send-message", s.conversationID, prompt)

		cmd := buildCommand(ctx, s.agentCmd, args, s.workspacePath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("调用 agentapi send-message 失败: %w, 输出: %s", err, string(out))
		}
	}

	return s.pollTranscript(ctx, s.transcriptPath(s.conversationID), startLineCount)
}

func (s *RealSession) Stop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	s.ready = false
	return nil
}

func (s *RealSession) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready && !s.stopped
}

func (s *RealSession) Events() <-chan acp.Event {
	return s.events
}

func (s *RealSession) ConversationID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conversationID
}

func (s *RealSession) transcriptPath(convID string) string {
	return filepath.Join(s.brainDir, convID, ".system_generated", "logs", "transcript.jsonl")
}

func (s *RealSession) pollTranscript(ctx context.Context, path string, startLine int) (string, error) {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			reply, done, err := scanResponse(path, &startLine, func(text string) {
				select {
				case s.events <- acp.Event{Type: "agent_activity", Payload: []byte(text)}:
				default:
				}
			})
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return "", err
			}
			if done {
				return reply, nil
			}
		}
	}
}

func readCompletedResponse(path string, startLine int) (string, bool, error) {
	return scanResponse(path, &startLine, nil)
}

func scanResponse(path string, consumed *int, progress func(string)) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	lineIdx := 0
	var lastPlannerContent string
	var hasNewDonePlanner bool

	for scanner.Scan() {
		line := scanner.Bytes()
		lineIdx++
		if lineIdx <= *consumed {
			continue
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var step TranscriptStep
		if err := json.Unmarshal(line, &step); err != nil {
			continue
		}

		*consumed = lineIdx
		if progress != nil && step.Source == "MODEL" && len(step.ToolCalls) > 0 {
			if step.Content != "" {
				progress(step.Content)
			}
			for _, raw := range step.ToolCalls {
				var tool struct{ Name string }
				if json.Unmarshal(raw, &tool) == nil && tool.Name != "" {
					progress("调用工具：" + tool.Name)
				}
			}
		}
		if step.Source == "MODEL" && step.Type == "PLANNER_RESPONSE" {
			if step.Status == "ERROR" {
				return "", true, fmt.Errorf("agent 发生错误: %s", step.Content)
			}
			if len(step.ToolCalls) == 0 && step.Content != "" && step.Status == "DONE" {
				lastPlannerContent = step.Content
				hasNewDonePlanner = true
			} else if len(step.ToolCalls) > 0 {
				// 还在执行工具中，不能视为已完成
				hasNewDonePlanner = false
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", false, err
	}
	if hasNewDonePlanner && lastPlannerContent != "" {
		return lastPlannerContent, true, nil
	}
	return "", false, nil
}

func countLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	count := 0
	for scanner.Scan() {
		count++
	}
	return count
}

func parseConversationID(out []byte) (string, error) {
	var resp struct {
		Response struct {
			NewConversation struct {
				ConversationID string `json:"conversationId"`
			} `json:"newConversation"`
		} `json:"response"`
	}
	if err := json.Unmarshal(out, &resp); err == nil && resp.Response.NewConversation.ConversationID != "" {
		return resp.Response.NewConversation.ConversationID, nil
	}

	// 降级尝试查找原始 JSON 块
	idx := bytes.Index(out, []byte(`"conversationId"`))
	if idx != -1 {
		start := bytes.LastIndex(out[:idx], []byte("{"))
		end := bytes.Index(out[idx:], []byte("}"))
		if start != -1 && end != -1 {
			chunk := out[start : idx+end+1]
			var m map[string]any
			if json.Unmarshal(chunk, &m) == nil {
				if cid, ok := m["conversationId"].(string); ok && cid != "" {
					return cid, nil
				}
			}
		}
	}
	return "", fmt.Errorf("未能从输出中提取 conversationId: %s", string(out))
}

func buildCommand(ctx context.Context, agentCmd string, args []string, dir string) *exec.Cmd {
	var cmd *exec.Cmd
	lower := strings.ToLower(agentCmd)
	if strings.HasSuffix(lower, ".bat") || strings.HasSuffix(lower, ".cmd") {
		cmdArgs := append([]string{"/c", agentCmd}, args...)
		cmd = exec.CommandContext(ctx, "cmd.exe", cmdArgs...)
	} else {
		cmd = exec.CommandContext(ctx, agentCmd, args...)
	}
	cmd.Env = runtimeenv.Base()
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd
}

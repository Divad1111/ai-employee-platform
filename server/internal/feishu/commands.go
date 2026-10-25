package feishu

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// CommandDirectory exposes only the employee/model data needed by chat commands.
// Commands never read secrets or depend on the concrete employee/workstation stores.
type CommandDirectory interface {
	CommandEmployee(context.Context, string) (CommandEmployee, error)
	CommandModels(context.Context, string) ([]CommandModel, error)
}

type CommandEmployee struct {
	ID, Name, Status, Provider, Model, WorkstationStatus string
	Assignable                                           bool
}

type CommandModel struct {
	Provider, ID, Label string
}

type commandSpec struct {
	path, usage, description string
	aliases                  []string
	handle                   func(*Service, context.Context, IncomingEvent, string, []string) string
}

// One registry owns matching, aliases, usage and help. Add a spec and a handler
// to extend the command surface; task dispatch does not need another branch.
func chatCommandSpecs() []commandSpec {
	return []commandSpec{
		{path: "/help", usage: "/help", description: "显示帮助", aliases: []string{"/h", "/?"}, handle: commandHelp},
		{path: "/list emp", usage: "/list emp", description: "列出已登记员工及可派单状态", aliases: []string{"/employees", "/list employees"}, handle: commandEmployees},
		{path: "/model", usage: "/model [员工]", description: "列出工作站上报的模型; 省略员工时按员工分组", aliases: []string{"/models"}, handle: commandModels},
		{path: "/status", usage: "/status <员工>", description: "查看员工状态及当前默认模型", handle: commandStatus},
		{path: "/whoami", usage: "/whoami", description: "查看当前飞书身份关联的员工", handle: commandWhoAmI},
		{path: "/files", usage: "/files", description: "查看待派单和最近任务已使用的附件", handle: commandPendingFiles},
		{path: "/clear files", usage: "/clear files", description: "清空自己在当前会话暂存的附件", handle: commandClearFiles},
		{path: "/change model", usage: "/change model <员工> [Provider] <模型>", description: "切换后续任务使用的模型(需已有身份绑定)", aliases: []string{"/change mode"}, handle: commandChangeModel},
		{path: "/new", usage: "/new <员工>", description: "新建会话, 下次任务使用新上下文(需已有身份绑定)", aliases: []string{"/new-session", "/new_session"}, handle: commandNewSession},
	}
}

func commandHelpMarkdown() string {
	var b strings.Builder
	b.WriteString("**机器人命令:**\n")
	for _, spec := range chatCommandSpecs() {
		fmt.Fprintf(&b, "• `%s` — %s\n", spec.usage, spec.description)
	}
	b.WriteString("\n员工可写别名、@别名或 EMP-xxxx。也支持 `@员工 /status`、`@员工 /model`、`@员工 /change model Provider 模型`、`@员工 /new`。\n`/change mode` 是 `/change model` 的兼容写法。模型名称含空格时直接填写完整名称，无需引号。\n")
	return b.String()
}

func matchCommand(fields []string) (*commandSpec, []string) {
	specs := chatCommandSpecs()
	for i := range specs {
		spec := &specs[i]
		for _, path := range append([]string{spec.path}, spec.aliases...) {
			parts := strings.Fields(path)
			if len(fields) < len(parts) {
				continue
			}
			match := true
			for j := range parts {
				match = match && strings.EqualFold(parts[j], fields[j])
			}
			if match {
				return spec, fields[len(parts):]
			}
		}
	}
	return nil, nil
}

// handleChatCommand runs before task creation, assignability checks and auto
// binding. An invalid control must never become an AI task or grant its author
// permission to modify an employee by implicitly binding that employee first.
func (s *Service) handleChatCommand(ctx context.Context, ev IncomingEvent) (bool, error) {
	text, scope := strings.TrimSpace(ev.Text), ""
	if !strings.HasPrefix(text, "/") {
		if id, prompt, err := s.ParseTarget(text); err == nil && strings.HasPrefix(strings.TrimSpace(prompt), "/") {
			text, scope = strings.TrimSpace(prompt), id
		} else {
			text = strings.TrimSpace(reLeadingMentions.ReplaceAllString(text, ""))
		}
	}
	fields := strings.Fields(text)
	if len(fields) > 0 && (strings.EqualFold(fields[0], "/emp") || strings.EqualFold(fields[0], "/employee")) {
		id, prompt, err := s.ParseTarget(ev.Text)
		if err != nil || !strings.HasPrefix(strings.TrimSpace(prompt), "/") {
			return false, nil
		}
		scope, fields = id, strings.Fields(prompt)
	}
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return false, nil
	}
	spec, args := matchCommand(fields)
	var content string
	if spec == nil {
		content = "未识别的命令。发送 `/help` 查看完整用法。\n\n" + commandHelpMarkdown()
	} else {
		content = spec.handle(s, ctx, ev, scope, args)
	}
	return true, s.sendCommandReply(ctx, ev.ChatID, content)
}

func (s *Service) sendCommandReply(ctx context.Context, chatID, content string) error {
	if s.Sender == nil || chatID == "" {
		return nil
	}
	// Leave room for card JSON and keep large workstation model catalogues usable.
	if runes := []rune(content); len(runes) > 10000 {
		content = string(runes[:10000]) + "\n…内容过长, 请用 /model 指定员工查询。"
	}
	return s.Sender.Send(ctx, Reply{ChatID: chatID, Content: NewMarkdownCard("员工助手", CardTemplateBlue, content, "AI Employee 企业员工协同平台").MustJSON()})
}

func commandHelp(_ *Service, _ context.Context, _ IncomingEvent, _ string, args []string) string {
	if len(args) != 0 {
		return "用法: `/help`\n\n" + commandHelpMarkdown()
	}
	return commandHelpMarkdown() + "\n派单: `@员工 任务需求`、`/emp 别名 任务需求` 或 `EMP-xxxx 任务需求`。"
}

func (s *Service) commandBindings() []Binding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var bindings []Binding
	for _, binding := range s.bindings {
		bindings = append(bindings, binding)
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].FeishuAlias < bindings[j].FeishuAlias })
	seen := map[string]bool{}
	var out []Binding
	for _, binding := range bindings {
		if !seen[binding.EmployeeID] {
			seen[binding.EmployeeID] = true
			out = append(out, binding)
		}
	}
	return out
}

func (s *Service) commandTarget(scope string, args []string) (string, []string, error) {
	if scope != "" {
		return scope, args, nil
	}
	if len(args) == 0 {
		return "", nil, fmt.Errorf("请指定员工别名或 EMP-xxxx; 可先发送 /list emp")
	}
	key := strings.TrimPrefix(args[0], "@")
	// Resolve exact tokens only; never infer an employee from a model or prompt.
	if strings.HasPrefix(strings.ToUpper(key), "EMP-") {
		return s.canonicalEmployeeID(key), args[1:], nil
	}
	s.mu.RLock()
	binding, exists := s.bindings[strings.ToLower(key)]
	s.mu.RUnlock()
	if !exists {
		return "", nil, fmt.Errorf("找不到该员工, 请发送 /list emp 查看已登记别名")
	}
	return binding.EmployeeID, args[1:], nil
}

// Generated IDs contain lowercase hexadecimal characters. Never uppercase the
// entire ID before querying the store; normalize known binding IDs instead.
func (s *Service) canonicalEmployeeID(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, binding := range s.bindings {
		if strings.EqualFold(binding.EmployeeID, key) {
			return binding.EmployeeID
		}
	}
	return "EMP-" + key[4:]
}

func commandEmployees(s *Service, ctx context.Context, _ IncomingEvent, _ string, args []string) string {
	if len(args) != 0 {
		return "用法: `/list emp`"
	}
	bindings := s.commandBindings()
	if len(bindings) == 0 {
		return "暂无已登记飞书别名的员工, 请在后台配置员工飞书绑定。"
	}
	var b strings.Builder
	b.WriteString("**员工列表:**\n")
	for _, binding := range bindings {
		state := "状态未知"
		if s.Commands != nil {
			if e, err := s.Commands.CommandEmployee(ctx, binding.EmployeeID); err == nil {
				state = "可派单"
				if !e.Assignable {
					state = "暂不可派单"
				}
				state += " / " + e.Status + " / 工作站 " + e.WorkstationStatus
			} else {
				state = "员工不可用"
			}
		}
		fmt.Fprintf(&b, "• `@%s` (`%s`) — %s\n", binding.FeishuAlias, binding.EmployeeID, state)
	}
	b.WriteString("\n用 `/status 员工` 查看模型配置, 用 `@员工 任务需求` 派单。")
	return b.String()
}

func commandStatus(s *Service, ctx context.Context, _ IncomingEvent, scope string, args []string) string {
	id, rest, err := s.commandTarget(scope, args)
	if err != nil {
		return err.Error()
	}
	if len(rest) != 0 {
		return "用法: `/status <员工>` 或 `@员工 /status`"
	}
	if s.Commands == nil {
		return "员工查询服务未配置。"
	}
	e, err := s.Commands.CommandEmployee(ctx, id)
	if err != nil {
		return "无法查询该员工。请发送 /list emp 检查员工是否存在。"
	}
	return fmt.Sprintf("**%s** (`%s`)\n员工状态: %s\n工作站状态: %s\n默认 Provider: `%s`\n默认模型: `%s`\n配置用于后续任务; 已运行任务以任务卡片显示为准。", e.Name, e.ID, e.Status, e.WorkstationStatus, displayDefault(e.Provider), displayDefault(e.Model))
}

func displayDefault(value string) string {
	if value == "" {
		return "默认"
	}
	return value
}

func commandModels(s *Service, ctx context.Context, ev IncomingEvent, scope string, args []string) string {
	// Preserve the previous employee-scoped /model <model> switch syntax.
	if scope != "" && len(args) > 0 {
		return s.employeeControl(ctx, ev, scope, "/model "+strings.Join(args, " "))
	}
	if s.Commands == nil {
		return "模型查询服务未配置。"
	}
	var ids []string
	if scope != "" || len(args) != 0 {
		id, rest, err := s.commandTarget(scope, args)
		if err != nil {
			return err.Error()
		}
		if len(rest) != 0 {
			return "用法: `/model [员工]` 或 `@员工 /model`"
		}
		ids = append(ids, id)
	} else {
		for _, binding := range s.commandBindings() {
			ids = append(ids, binding.EmployeeID)
		}
	}
	if len(ids) == 0 {
		return "暂无可查询的员工, 请先配置员工和工作站绑定。"
	}
	var b strings.Builder
	b.WriteString("**模型列表(工作站最近上报):**\n")
	for _, id := range ids {
		fmt.Fprintf(&b, "\n**%s** (`%s`)\n", s.getEmployeeDisplayName(ctx, id), id)
		models, err := s.Commands.CommandModels(ctx, id)
		if err != nil {
			b.WriteString("无法读取模型列表, 请检查员工和工作站绑定。\n")
			continue
		}
		if len(models) == 0 {
			b.WriteString("工作站尚未上报模型, 请确认在线并在后台刷新模型。\n")
		}
		for _, model := range models {
			fmt.Fprintf(&b, "• `%s` / `%s`", model.Provider, model.ID)
			if model.Label != "" && model.Label != model.ID {
				fmt.Fprintf(&b, " (%s)", model.Label)
			}
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n切换: `/change model 员工 Provider 模型ID`。离线工作站可能显示缓存列表, 实际可用性以工作站为准。")
	return b.String()
}

func commandWhoAmI(s *Service, _ context.Context, ev IncomingEvent, _ string, args []string) string {
	if len(args) != 0 {
		return "用法: `/whoami`"
	}
	var aliases []string
	for _, binding := range s.commandBindings() {
		if ev.SenderOpenID != "" && s.EmployeeHasOpenID(binding.EmployeeID, ev.SenderOpenID) {
			aliases = append(aliases, "@"+binding.FeishuAlias+" ("+binding.EmployeeID+")")
		}
	}
	if len(aliases) == 0 {
		return "当前飞书账号尚未关联员工身份。切换模型和新建会话前, 请在后台完成身份绑定。"
	}
	return "当前飞书账号已关联员工:\n" + strings.Join(aliases, "\n")
}

func commandChangeModel(s *Service, ctx context.Context, ev IncomingEvent, scope string, args []string) string {
	id, rest, err := s.commandTarget(scope, args)
	if err != nil {
		return err.Error()
	}
	if len(rest) == 0 {
		return "用法: `/change model <员工> [Provider] <模型>`; 可先发送 `/model 员工`。"
	}
	return s.employeeControl(ctx, ev, id, "/model "+strings.Join(rest, " "))
}

func commandNewSession(s *Service, ctx context.Context, ev IncomingEvent, scope string, args []string) string {
	id, rest, err := s.commandTarget(scope, args)
	if err != nil {
		return err.Error()
	}
	if len(rest) != 0 {
		return "用法: `/new <员工>` 或 `@员工 /new`"
	}
	return s.employeeControl(ctx, ev, id, "/new-session")
}

func (s *Service) employeeControl(ctx context.Context, ev IncomingEvent, id, command string) string {
	if !s.TaskSenderAllowed(id, ev.SenderOpenID, ev.ChatID, !isP2PChat(ev.ChatType)) || s.AuthorizeActor == nil || s.AuthorizeActor(ctx, id, ev.SenderOpenID) != nil {
		return "当前飞书账号未绑定该 AI 员工, 不能修改会话或模型。请在后台完成身份绑定。"
	}
	s.mu.RLock()
	handler := s.TaskConversations
	s.mu.RUnlock()
	if handler == nil {
		return "员工会话服务未配置。"
	}
	if reply, handled := handler.HandleEmployeeCommand(ctx, id, command, ev.SenderOpenID); handled {
		return reply
	}
	return "员工会话服务不支持该操作。"
}

package manageragent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	maxAgentRounds      = 8
	maxUserMessageBytes = 8 * 1024
	maxToolResultBytes  = 32 * 1024
	maxSessionMessages  = 80
	maxSessions         = 32
	sessionTTL          = 6 * time.Hour
)

const systemPrompt = `你是 LongHub Manager 内置管家，独立于 OpenClaw Gateway 运行。
你的职责是诊断、解释并通过提供的固定工具修复本机 OpenClaw。
规则：
1. 先检查再行动，不得声称执行了没有工具结果支持的操作。
2. 每次只请求一个会修改系统的工具；写操作必须等待用户明确批准。
3. 用户拒绝后尊重拒绝，不得换一种写工具绕过。
4. 不索要或回显 API Key、Token、密码；不要输出本地绝对路径。
5. OpenClaw 未安装时先使用安装预检，再建议安装；配置异常时先诊断再修复。
6. 回答使用简洁中文，说明发现、行动结果和下一步。`

var agentSecretPattern = regexp.MustCompile(`(?i)(bearer\s+|(?:api[_-]?key|token|password|secret)\s*[:=]\s*)[^\s,;]+`)
var agentCredentialPattern = regexp.MustCompile(`(?i)\b(?:sk|xoxb|xoxp|ghp|github_pat)-[a-z0-9_-]{12,}`)
var agentWindowsHomePattern = regexp.MustCompile(`(?i)[a-z]:\\users\\[^\\/\s]+`)
var agentUnixHomePattern = regexp.MustCompile(`(?:/home|/Users)/[^/\s]+`)

type ToolSpec struct {
	Definition       ToolDefinition
	RequiresApproval bool
	ApprovalSummary  string
}

type ToolExecutor interface {
	Specs() []ToolSpec
	Execute(context.Context, string, json.RawMessage) (string, error)
}

type Approval struct {
	ID      string          `json:"id"`
	Tool    string          `json:"tool"`
	Summary string          `json:"summary"`
	Args    json.RawMessage `json:"args"`
}

type Step struct {
	Tool    string `json:"tool"`
	Summary string `json:"summary"`
	Success bool   `json:"success"`
}

type TurnResponse struct {
	SessionID string    `json:"session_id"`
	Reply     string    `json:"reply,omitempty"`
	Steps     []Step    `json:"steps,omitempty"`
	Approval  *Approval `json:"approval,omitempty"`
	Done      bool      `json:"done"`
}

type pendingState struct {
	approval  Approval
	call      ToolCall
	remaining []ToolCall
}

type agentSession struct {
	mu       sync.Mutex
	messages []Message
	pending  *pendingState
	updated  atomic.Int64
}

type Engine struct {
	config   *ConfigStore
	model    *ModelClient
	executor ToolExecutor
	specs    map[string]ToolSpec
	tools    []ToolDefinition
	mu       sync.Mutex
	sessions map[string]*agentSession
	now      func() time.Time
}

func NewEngine(config *ConfigStore, model *ModelClient, executor ToolExecutor) (*Engine, error) {
	if config == nil || model == nil || executor == nil {
		return nil, errors.New("agent dependencies are required")
	}
	specs := make(map[string]ToolSpec)
	tools := make([]ToolDefinition, 0)
	for _, spec := range executor.Specs() {
		name := spec.Definition.Function.Name
		if name == "" {
			return nil, errors.New("agent tool name is required")
		}
		if _, exists := specs[name]; exists {
			return nil, fmt.Errorf("duplicate agent tool: %s", name)
		}
		specs[name] = spec
		tools = append(tools, spec.Definition)
	}
	return &Engine{
		config: config, model: model, executor: executor, specs: specs, tools: tools,
		sessions: make(map[string]*agentSession), now: time.Now,
	}, nil
}

// TestConnection verifies the saved credentials against the same model,
// protocol and tool definitions used by a real Manager Agent turn. It does not
// create a session or execute a tool.
func (e *Engine) TestConnection(ctx context.Context) error {
	config, apiKey, err := e.config.Credentials()
	if err != nil {
		return err
	}
	_, err = e.model.Complete(ctx, config, apiKey, []Message{
		{Role: "system", Content: "这是连接测试。请只回复“连接正常”，不要调用工具。"},
		{Role: "user", Content: "测试连接"},
	}, e.tools)
	return err
}

// PublicErrorMessage returns a bounded, redacted error that is safe to show in
// the local WebView. Provider details are useful for distinguishing invalid
// credentials, unavailable models and protocol errors, but secrets and user
// home paths must never cross the API boundary.
func PublicErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return sanitizeModelText(sanitizeAgentText(err.Error()), "")
}

func (e *Engine) Turn(ctx context.Context, sessionID, userMessage string) (TurnResponse, error) {
	userMessage = strings.TrimSpace(userMessage)
	if userMessage == "" || len(userMessage) > maxUserMessageBytes || strings.ContainsRune(userMessage, '\x00') {
		return TurnResponse{}, errors.New("消息为空或超过大小限制")
	}
	newSession := strings.TrimSpace(sessionID) == ""
	sessionID, session, err := e.session(sessionID)
	if err != nil {
		return TurnResponse{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.pending != nil {
		return TurnResponse{}, errors.New("当前会话仍有待确认操作")
	}
	session.messages = append(session.messages, Message{Role: "user", Content: userMessage})
	session.updated.Store(e.now().UnixNano())
	response, err := e.continueLocked(ctx, sessionID, session, nil)
	if err != nil && newSession {
		e.Reset(sessionID)
	}
	return response, err
}

func (e *Engine) ResolveApproval(ctx context.Context, sessionID, approvalID string, approved bool) (TurnResponse, error) {
	_, session, err := e.sessionExisting(sessionID)
	if err != nil {
		return TurnResponse{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	pending := session.pending
	if pending == nil || pending.approval.ID != approvalID {
		return TurnResponse{}, errors.New("待确认操作不存在或已过期")
	}
	session.pending = nil
	steps := make([]Step, 0, 1)
	if approved {
		result, runErr := e.executeTool(ctx, pending.call)
		steps = append(steps, Step{Tool: pending.call.Function.Name, Summary: pending.approval.Summary, Success: runErr == nil})
		result = toolResultContent(result, runErr)
		session.messages = append(session.messages, Message{Role: "tool", ToolCallID: pending.call.ID, Content: result})
	} else {
		steps = append(steps, Step{Tool: pending.call.Function.Name, Summary: "用户拒绝执行", Success: false})
		session.messages = append(session.messages, Message{Role: "tool", ToolCallID: pending.call.ID, Content: "用户拒绝了此操作。不要再次请求同一操作，除非用户明确改变决定。"})
	}
	response, paused, err := e.processCallsLocked(ctx, sessionID, session, pending.remaining, steps)
	if err != nil {
		return e.approvalSummaryFallback(sessionID, session, steps, approved), nil
	}
	if paused {
		return response, err
	}
	final, err := e.continueLocked(ctx, sessionID, session, response.Steps)
	if err != nil {
		return e.approvalSummaryFallback(sessionID, session, response.Steps, approved), nil
	}
	return final, nil
}

func (e *Engine) approvalSummaryFallback(sessionID string, session *agentSession, steps []Step, approved bool) TurnResponse {
	reply := "操作已拒绝；管家模型未能生成后续总结。"
	if approved && len(steps) > 0 && steps[0].Success {
		reply = "操作已经执行，但管家模型未能生成总结。请刷新对应页面确认最新状态。"
	} else if approved {
		reply = "操作执行失败，且管家模型未能生成总结。请查看步骤状态并重新诊断。"
	}
	e.compactLocked(session)
	return TurnResponse{SessionID: sessionID, Reply: reply, Steps: steps, Done: true}
}

func (e *Engine) Reset(sessionID string) {
	e.mu.Lock()
	delete(e.sessions, sessionID)
	e.mu.Unlock()
}

func (e *Engine) continueLocked(ctx context.Context, sessionID string, session *agentSession, steps []Step) (TurnResponse, error) {
	for round := 0; round < maxAgentRounds; round++ {
		config, apiKey, err := e.config.Credentials()
		if err != nil {
			return TurnResponse{}, err
		}
		messages := make([]Message, 0, len(session.messages)+1)
		messages = append(messages, Message{Role: "system", Content: systemPrompt})
		messages = append(messages, session.messages...)
		assistant, err := e.model.Complete(ctx, config, apiKey, messages, e.tools)
		if err != nil {
			return TurnResponse{}, err
		}
		session.messages = append(session.messages, assistant)
		session.updated.Store(e.now().UnixNano())
		if len(assistant.ToolCalls) == 0 {
			if strings.TrimSpace(assistant.Content) == "" {
				return TurnResponse{}, errors.New("管家模型返回了空回复")
			}
			e.compactLocked(session)
			return TurnResponse{SessionID: sessionID, Reply: strings.TrimSpace(assistant.Content), Steps: steps, Done: true}, nil
		}
		response, paused, err := e.processCallsLocked(ctx, sessionID, session, assistant.ToolCalls, steps)
		if err != nil || paused {
			return response, err
		}
		steps = response.Steps
	}
	return TurnResponse{}, errors.New("管家连续工具调用超过安全轮次限制")
}

func (e *Engine) processCallsLocked(
	ctx context.Context,
	sessionID string,
	session *agentSession,
	calls []ToolCall,
	steps []Step,
) (TurnResponse, bool, error) {
	for index, call := range calls {
		spec, ok := e.specs[call.Function.Name]
		if !ok {
			session.messages = append(session.messages, Message{Role: "tool", ToolCallID: call.ID, Content: "未知或未授权的工具。"})
			steps = append(steps, Step{Tool: call.Function.Name, Summary: "拒绝未知工具", Success: false})
			continue
		}
		args := json.RawMessage(call.Function.Arguments)
		var arguments map[string]any
		if !json.Valid(args) || json.Unmarshal(args, &arguments) != nil || arguments == nil {
			session.messages = append(session.messages, Message{Role: "tool", ToolCallID: call.ID, Content: "工具参数不是有效 JSON。"})
			steps = append(steps, Step{Tool: call.Function.Name, Summary: "参数无效", Success: false})
			continue
		}
		if spec.RequiresApproval {
			approvalID, err := randomID()
			if err != nil {
				return TurnResponse{}, false, err
			}
			approval := Approval{ID: approvalID, Tool: call.Function.Name, Summary: spec.ApprovalSummary, Args: args}
			session.pending = &pendingState{approval: approval, call: call, remaining: append([]ToolCall(nil), calls[index+1:]...)}
			return TurnResponse{SessionID: sessionID, Steps: steps, Approval: &approval, Done: false}, true, nil
		}
		result, runErr := e.executeTool(ctx, call)
		steps = append(steps, Step{Tool: call.Function.Name, Summary: spec.ApprovalSummary, Success: runErr == nil})
		result = toolResultContent(result, runErr)
		session.messages = append(session.messages, Message{Role: "tool", ToolCallID: call.ID, Content: result})
	}
	return TurnResponse{SessionID: sessionID, Steps: steps}, false, nil
}

func (e *Engine) executeTool(ctx context.Context, call ToolCall) (string, error) {
	result, err := e.executor.Execute(ctx, call.Function.Name, json.RawMessage(call.Function.Arguments))
	result = sanitizeAgentText(result)
	if len(result) > maxToolResultBytes {
		end := maxToolResultBytes
		for end > 0 && !utf8.ValidString(result[:end]) {
			end--
		}
		result = result[:end] + "\n[结果已由 LongHub 截断]"
	}
	return result, err
}

func toolResultContent(result string, err error) string {
	if err == nil {
		return result
	}
	message := "工具执行失败：" + sanitizeAgentText(err.Error())
	if strings.TrimSpace(result) == "" {
		return message
	}
	return result + "\n" + message
}

func sanitizeAgentText(value string) string {
	value = agentSecretPattern.ReplaceAllString(value, "${1}[REDACTED]")
	value = agentCredentialPattern.ReplaceAllString(value, "[REDACTED]")
	value = agentWindowsHomePattern.ReplaceAllString(value, "~")
	return agentUnixHomePattern.ReplaceAllString(value, "~")
}

func (e *Engine) session(sessionID string) (string, *agentSession, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pruneExpiredLocked()
	if strings.TrimSpace(sessionID) == "" {
		e.evictForNewSessionLocked()
		id, err := randomID()
		if err != nil {
			return "", nil, err
		}
		session := &agentSession{}
		session.updated.Store(e.now().UnixNano())
		e.sessions[id] = session
		return id, session, nil
	}
	session, ok := e.sessions[sessionID]
	if !ok {
		return "", nil, errors.New("管家会话不存在或已过期")
	}
	return sessionID, session, nil
}

func (e *Engine) sessionExisting(sessionID string) (string, *agentSession, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", nil, errors.New("管家会话 ID 不能为空")
	}
	return e.session(sessionID)
}

func (e *Engine) pruneExpiredLocked() {
	cutoff := e.now().Add(-sessionTTL).UnixNano()
	for id, session := range e.sessions {
		if session.updated.Load() < cutoff {
			delete(e.sessions, id)
		}
	}
}

func (e *Engine) evictForNewSessionLocked() {
	for len(e.sessions) >= maxSessions {
		var oldestID string
		var oldest int64
		for id, session := range e.sessions {
			updated := session.updated.Load()
			if oldestID == "" || updated < oldest {
				oldestID, oldest = id, updated
			}
		}
		delete(e.sessions, oldestID)
	}
}

func (e *Engine) compactLocked(session *agentSession) {
	if len(session.messages) <= maxSessionMessages {
		return
	}
	start := len(session.messages) - maxSessionMessages
	for start < len(session.messages) && session.messages[start].Role != "user" {
		start++
	}
	if start == len(session.messages) {
		return
	}
	session.messages = append([]Message(nil), session.messages[start:]...)
}

func randomID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

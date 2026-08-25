package manageragent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxModelRequestBytes   = 3 * 1024 * 1024
	maxModelResponseBytes  = 2 * 1024 * 1024
	maxModelMessageBytes   = 64 * 1024
	maxResponsesStateBytes = 256 * 1024
	maxModelToolCalls      = 16
)

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Message struct {
	Role          string            `json:"role"`
	Content       string            `json:"content,omitempty"`
	ToolCallID    string            `json:"tool_call_id,omitempty"`
	ToolCalls     []ToolCall        `json:"tool_calls,omitempty"`
	ResponseItems []json.RawMessage `json:"-"`
}

type ToolDefinition struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ModelClient struct {
	httpClient *http.Client
}

func NewModelClient(client *http.Client) *ModelClient {
	if client == nil {
		client = &http.Client{}
	}
	copy := *client
	if copy.Timeout == 0 {
		copy.Timeout = 90 * time.Second
	}
	copy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &ModelClient{httpClient: &copy}
}

func (c *ModelClient) Complete(
	ctx context.Context,
	config Config,
	apiKey string,
	messages []Message,
	tools []ToolDefinition,
) (Message, error) {
	switch effectiveProtocol(config) {
	case ProtocolResponses:
		return c.completeResponses(ctx, config, apiKey, messages, tools)
	default:
		return c.completeChatCompletions(ctx, config, apiKey, messages, tools)
	}
}

func effectiveProtocol(config Config) string {
	if config.Protocol == ProtocolResponses || config.Protocol == ProtocolChatCompletions {
		return config.Protocol
	}
	parsed, err := url.Parse(config.BaseURL)
	if err == nil {
		path := strings.ToLower(strings.TrimRight(parsed.Path, "/"))
		if strings.HasSuffix(path, "/responses") || strings.Contains(path, "/codex/") {
			return ProtocolResponses
		}
	}
	return ProtocolChatCompletions
}

func (c *ModelClient) completeChatCompletions(
	ctx context.Context,
	config Config,
	apiKey string,
	messages []Message,
	tools []ToolDefinition,
) (Message, error) {
	endpoint, err := completionEndpoint(config.BaseURL)
	if err != nil {
		return Message{}, err
	}
	payload := map[string]any{
		"model":       config.Model,
		"messages":    messages,
		"tools":       tools,
		"tool_choice": "auto",
		"temperature": 0.1,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Message{}, err
	}
	if len(encoded) > maxModelRequestBytes {
		return Message{}, errors.New("管家模型请求超过大小限制")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("调用管家模型失败: %w", err)
	}
	defer res.Body.Close()
	reader := io.LimitReader(res.Body, maxModelResponseBytes+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return Message{}, errors.New("读取管家模型响应失败")
	}
	if len(data) > maxModelResponseBytes {
		return Message{}, errors.New("管家模型响应超过大小限制")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Message{}, fmt.Errorf("管家模型返回 HTTP %d: %s", res.StatusCode, safeModelError(data, apiKey))
	}
	var response struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return Message{}, errors.New("管家模型响应不是有效 JSON")
	}
	if response.Error != nil {
		return Message{}, errors.New(sanitizeModelText(response.Error.Message, apiKey))
	}
	if len(response.Choices) == 0 || response.Choices[0].Message.Role == "" {
		return Message{}, errors.New("管家模型没有返回可用消息")
	}
	message := response.Choices[0].Message
	return validateModelMessage(message)
}

func (c *ModelClient) completeResponses(
	ctx context.Context,
	config Config,
	apiKey string,
	messages []Message,
	tools []ToolDefinition,
) (Message, error) {
	endpoint, err := responsesEndpoint(config.BaseURL)
	if err != nil {
		return Message{}, err
	}
	responseTools := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		responseTools = append(responseTools, map[string]any{
			"type":        "function",
			"name":        tool.Function.Name,
			"description": tool.Function.Description,
			"parameters":  tool.Function.Parameters,
		})
	}
	payload := map[string]any{
		"model":       config.Model,
		"input":       responsesInput(messages),
		"tools":       responseTools,
		"tool_choice": "auto",
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Message{}, err
	}
	if len(encoded) > maxModelRequestBytes {
		return Message{}, errors.New("管家模型请求超过大小限制")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("调用管家模型失败: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxModelResponseBytes+1))
	if err != nil {
		return Message{}, errors.New("读取管家模型响应失败")
	}
	if len(data) > maxModelResponseBytes {
		return Message{}, errors.New("管家模型响应超过大小限制")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Message{}, fmt.Errorf("管家模型返回 HTTP %d: %s", res.StatusCode, safeModelError(data, apiKey))
	}
	var response struct {
		Output []json.RawMessage `json:"output"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return Message{}, errors.New("管家模型响应不是有效 JSON")
	}
	if response.Error != nil {
		return Message{}, errors.New(sanitizeModelText(response.Error.Message, apiKey))
	}
	message := Message{Role: "assistant"}
	textParts := make([]string, 0)
	stateBytes := 0
	for _, rawItem := range response.Output {
		var item struct {
			ID        string `json:"id"`
			Type      string `json:"type"`
			Role      string `json:"role"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(rawItem, &item) != nil {
			return Message{}, errors.New("管家模型返回了无效输出项")
		}
		switch item.Type {
		case "message":
			if item.Role != "" && item.Role != "assistant" {
				return Message{}, errors.New("管家模型返回了无效角色")
			}
			for _, content := range item.Content {
				if content.Type == "output_text" && content.Text != "" {
					textParts = append(textParts, content.Text)
				}
			}
		case "function_call":
			callID := item.CallID
			if callID == "" {
				callID = item.ID
			}
			message.ToolCalls = append(message.ToolCalls, ToolCall{
				ID: callID, Type: "function",
				Function: FunctionCall{Name: item.Name, Arguments: item.Arguments},
			})
		}
		if item.Type == "message" || item.Type == "function_call" || item.Type == "reasoning" {
			stateBytes += len(rawItem)
			if stateBytes > maxResponsesStateBytes {
				return Message{}, errors.New("管家模型 Responses 状态超过大小限制")
			}
			message.ResponseItems = append(message.ResponseItems, append(json.RawMessage(nil), rawItem...))
		}
	}
	message.Content = strings.Join(textParts, "\n")
	if message.Content == "" && len(message.ToolCalls) == 0 {
		return Message{}, errors.New("管家模型没有返回可用消息")
	}
	return validateModelMessage(message)
}

func responsesInput(messages []Message) []any {
	input := make([]any, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case "system", "user":
			input = append(input, map[string]any{"role": message.Role, "content": message.Content})
		case "assistant":
			if len(message.ResponseItems) > 0 {
				for _, item := range message.ResponseItems {
					input = append(input, item)
				}
				continue
			}
			if message.Content != "" {
				input = append(input, map[string]any{"role": "assistant", "content": message.Content})
			}
			for _, call := range message.ToolCalls {
				input = append(input, map[string]any{
					"type": "function_call", "call_id": call.ID,
					"name": call.Function.Name, "arguments": call.Function.Arguments,
				})
			}
		case "tool":
			input = append(input, map[string]any{
				"type": "function_call_output", "call_id": message.ToolCallID, "output": message.Content,
			})
		}
	}
	return input
}

func validateModelMessage(message Message) (Message, error) {
	if message.Role != "assistant" {
		return Message{}, errors.New("管家模型返回了无效角色")
	}
	stateBytes := 0
	for _, item := range message.ResponseItems {
		stateBytes += len(item)
	}
	if len(message.Content) > maxModelMessageBytes || len(message.ToolCalls) > maxModelToolCalls || stateBytes > maxResponsesStateBytes {
		return Message{}, errors.New("管家模型消息超过大小限制")
	}
	callIDs := make(map[string]struct{}, len(message.ToolCalls))
	for _, call := range message.ToolCalls {
		if call.ID == "" || call.Type != "function" || call.Function.Name == "" || len(call.Function.Arguments) > 16*1024 {
			return Message{}, errors.New("管家模型返回了无效工具调用")
		}
		if _, exists := callIDs[call.ID]; exists {
			return Message{}, errors.New("管家模型返回了重复工具调用")
		}
		callIDs[call.ID] = struct{}{}
	}
	return message, nil
}

func responsesEndpoint(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(path, "/responses"):
	case strings.HasSuffix(path, "/chat/completions"):
		parsed.Path = strings.TrimSuffix(path, "/chat/completions") + "/responses"
	case strings.HasSuffix(path, "/v1"):
		parsed.Path = path + "/responses"
	case path == "":
		parsed.Path = "/v1/responses"
	default:
		parsed.Path = path + "/responses"
	}
	return parsed.String(), nil
}

func completionEndpoint(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
	case strings.HasSuffix(path, "/responses"):
		parsed.Path = strings.TrimSuffix(path, "/responses") + "/chat/completions"
	case strings.HasSuffix(path, "/v1"):
		parsed.Path = path + "/chat/completions"
	case path == "":
		parsed.Path = "/v1/chat/completions"
	default:
		parsed.Path = path + "/chat/completions"
	}
	return parsed.String(), nil
}

func safeModelError(data []byte, apiKey string) string {
	value := strings.TrimSpace(string(data))
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &envelope) == nil && strings.TrimSpace(envelope.Error.Message) != "" {
		value = envelope.Error.Message
	}
	return sanitizeModelText(value, apiKey)
}

func sanitizeModelText(value, apiKey string) string {
	value = strings.TrimSpace(value)
	if apiKey != "" {
		value = strings.ReplaceAll(value, apiKey, "[REDACTED]")
	}
	if len(value) > 512 {
		value = value[:512]
	}
	value = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' || (r >= 0x20 && r != 0x7f) {
			return r
		}
		return ' '
	}, value)
	if value == "" {
		return "模型服务未提供错误详情"
	}
	return value
}

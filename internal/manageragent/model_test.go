package manageragent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelClientCallsOpenAICompatibleEndpoint(t *testing.T) {
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["model"] != "manager-model" {
			t.Errorf("unexpected model: %#v", payload["model"])
		}
		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"inspect_runtime","arguments":"{}"}}]}}]}`)
	}))
	defer server.Close()

	client := NewModelClient(server.Client())
	message, err := client.Complete(t.Context(), Config{BaseURL: server.URL + "/v1", Model: "manager-model"}, "secret-key", []Message{{Role: "user", Content: "检查"}}, []ToolDefinition{{Type: "function", Function: ToolFunction{Name: "inspect_runtime", Parameters: map[string]any{"type": "object"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/chat/completions" || gotAuth != "Bearer secret-key" || len(message.ToolCalls) != 1 {
		t.Fatalf("unexpected request/result path=%q auth=%q message=%#v", gotPath, gotAuth, message)
	}
}

func TestModelClientUsesResponsesForCodexEndpoint(t *testing.T) {
	var gotPath string
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		gotPath = r.URL.Path
		var payload struct {
			Input []struct {
				Type   string `json:"type"`
				CallID string `json:"call_id"`
				Output string `json:"output"`
			} `json:"input"`
			Tools []struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if len(payload.Tools) != 1 || payload.Tools[0].Type != "function" || payload.Tools[0].Name != "inspect_runtime" {
			t.Errorf("unexpected Responses tools: %#v", payload.Tools)
		}
		foundOutput := false
		foundReasoning := false
		expectedCallID := "call-old"
		if requestCount > 1 {
			expectedCallID = "call-new"
		}
		for _, input := range payload.Input {
			if input.Type == "function_call_output" && input.CallID == expectedCallID && input.Output == "done" {
				foundOutput = true
			}
			if input.Type == "reasoning" {
				foundReasoning = true
			}
		}
		if requestCount == 1 && !foundOutput {
			t.Errorf("Responses input did not preserve tool output: %#v", payload.Input)
		}
		w.Header().Set("content-type", "application/json")
		if requestCount == 1 {
			_, _ = io.WriteString(w, `{"output":[{"type":"reasoning","id":"rs-1","encrypted_content":"opaque"},{"type":"function_call","id":"fc-1","call_id":"call-new","name":"inspect_runtime","arguments":"{}"}]}`)
			return
		}
		if !foundReasoning || !foundOutput {
			t.Errorf("Responses continuation lost state: %#v", payload.Input)
		}
		_, _ = io.WriteString(w, `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}`)
	}))
	defer server.Close()

	messages := []Message{
		{Role: "user", Content: "check"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call-old", Type: "function", Function: FunctionCall{Name: "inspect_runtime", Arguments: "{}"}}}},
		{Role: "tool", ToolCallID: "call-old", Content: "done"},
	}
	tools := []ToolDefinition{{Type: "function", Function: ToolFunction{Name: "inspect_runtime", Parameters: map[string]any{"type": "object"}}}}
	message, err := NewModelClient(server.Client()).Complete(t.Context(), Config{
		BaseURL: server.URL + "/codex/v1", Model: "codex-model", Protocol: ProtocolAuto,
	}, "secret-key", messages, tools)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/codex/v1/responses" || len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "call-new" {
		t.Fatalf("unexpected Responses result path=%q message=%#v", gotPath, message)
	}
	continued, err := NewModelClient(server.Client()).Complete(t.Context(), Config{
		BaseURL: server.URL + "/codex/v1", Model: "codex-model", Protocol: ProtocolAuto,
	}, "secret-key", []Message{message, {Role: "tool", ToolCallID: "call-new", Content: "done"}}, tools)
	if err != nil || continued.Content != "done" {
		t.Fatalf("unexpected Responses continuation: message=%#v err=%v", continued, err)
	}
}

func TestModelClientParsesResponsesText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"连接正常"}]}]}`)
	}))
	defer server.Close()
	message, err := NewModelClient(server.Client()).Complete(t.Context(), Config{
		BaseURL: server.URL + "/v1", Model: "model", Protocol: ProtocolResponses,
	}, "key", []Message{{Role: "user", Content: "test"}}, nil)
	if err != nil || message.Content != "连接正常" {
		t.Fatalf("unexpected Responses text: message=%#v err=%v", message, err)
	}
}

func TestExplicitChatProtocolOverridesCodexHeuristic(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer server.Close()
	_, err := NewModelClient(server.Client()).Complete(t.Context(), Config{
		BaseURL: server.URL + "/codex/v1", Model: "model", Protocol: ProtocolChatCompletions,
	}, "key", nil, nil)
	if err != nil || gotPath != "/codex/v1/chat/completions" {
		t.Fatalf("explicit chat protocol was not honored: path=%q err=%v", gotPath, err)
	}
}

func TestModelClientRejectsRedirects(t *testing.T) {
	redirected := false
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected = true }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	_, err := NewModelClient(server.Client()).Complete(t.Context(), Config{BaseURL: server.URL, Model: "model"}, "key", nil, nil)
	if err == nil || redirected {
		t.Fatalf("redirect should be rejected without forwarding credentials: err=%v redirected=%v", err, redirected)
	}
}

func TestModelClientRedactsAPIKeyFromProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid secret-key-value"}}`)
	}))
	defer server.Close()

	_, err := NewModelClient(server.Client()).Complete(t.Context(), Config{BaseURL: server.URL, Model: "model"}, "secret-key-value", nil, nil)
	if err == nil || strings.Contains(err.Error(), "secret-key-value") || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("provider error was not safely redacted: %v", err)
	}
}

func TestModelClientRejectsOversizedRequestBeforeNetwork(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	_, err := NewModelClient(server.Client()).Complete(
		t.Context(), Config{BaseURL: server.URL, Model: "model"}, "key",
		[]Message{{Role: "user", Content: strings.Repeat("x", maxModelRequestBytes+1)}}, nil,
	)
	if err == nil || called {
		t.Fatalf("oversized model request reached network: err=%v called=%v", err, called)
	}
}

func TestModelClientLimitsToolCallsPerMessage(t *testing.T) {
	calls := make([]ToolCall, maxModelToolCalls+1)
	for index := range calls {
		calls[index] = ToolCall{
			ID: "call", Type: "function",
			Function: FunctionCall{Name: "inspect", Arguments: "{}"},
		}
	}
	response, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": Message{Role: "assistant", ToolCalls: calls}}},
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(response) }))
	defer server.Close()
	if _, err := NewModelClient(server.Client()).Complete(t.Context(), Config{BaseURL: server.URL, Model: "model"}, "key", nil, nil); err == nil {
		t.Fatal("excessive tool calls were accepted")
	}
}

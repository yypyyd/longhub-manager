package manageragent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

type testToolExecutor struct {
	mu       sync.Mutex
	executed []string
}

func (e *testToolExecutor) Specs() []ToolSpec {
	empty := map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
	return []ToolSpec{
		{Definition: ToolDefinition{Type: "function", Function: ToolFunction{Name: "read_status", Description: "read", Parameters: empty}}, ApprovalSummary: "读取状态"},
		{Definition: ToolDefinition{Type: "function", Function: ToolFunction{Name: "write_repair", Description: "write", Parameters: empty}}, RequiresApproval: true, ApprovalSummary: "执行修复"},
	}
}

func (e *testToolExecutor) Execute(_ context.Context, name string, args json.RawMessage) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.executed = append(e.executed, name)
	return `{"ok":true}`, nil
}

func newScriptedEngine(t *testing.T, responses ...string) (*Engine, *testToolExecutor) {
	t.Helper()
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if len(responses) == 0 {
			t.Error("model received more requests than expected")
			http.Error(w, "no response", http.StatusInternalServerError)
			return
		}
		next := responses[0]
		responses = responses[1:]
		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, next)
	}))
	t.Cleanup(server.Close)
	config, err := NewConfigStore(filepath.Join(t.TempDir(), "agent.json"), &memorySecretStore{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Save(server.URL, "test-model", "test-key"); err != nil {
		t.Fatal(err)
	}
	executor := &testToolExecutor{}
	engine, err := NewEngine(config, NewModelClient(server.Client()), executor)
	if err != nil {
		t.Fatal(err)
	}
	return engine, executor
}

func modelReply(content string) string {
	encoded, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}}})
	return string(encoded)
}

func modelToolCall(name string) string {
	encoded, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{
		"role": "assistant", "tool_calls": []any{map[string]any{
			"id": "call-" + name, "type": "function", "function": map[string]any{"name": name, "arguments": "{}"},
		}},
	}}}})
	return string(encoded)
}

func TestEngineRunsReadToolAndReturnsGroundedReply(t *testing.T) {
	engine, executor := newScriptedEngine(t, modelToolCall("read_status"), modelReply("状态正常。"))
	response, err := engine.Turn(t.Context(), "", "请检查状态")
	if err != nil {
		t.Fatal(err)
	}
	if !response.Done || response.SessionID == "" || response.Reply != "状态正常。" || len(response.Steps) != 1 || !response.Steps[0].Success {
		t.Fatalf("unexpected response: %#v", response)
	}
	if len(executor.executed) != 1 || executor.executed[0] != "read_status" {
		t.Fatalf("read tool not executed: %#v", executor.executed)
	}
}

func TestEngineRequiresApprovalForWriteTool(t *testing.T) {
	engine, executor := newScriptedEngine(t, modelToolCall("write_repair"), modelReply("修复完成。"))
	first, err := engine.Turn(t.Context(), "", "修复它")
	if err != nil {
		t.Fatal(err)
	}
	if first.Done || first.Approval == nil || len(executor.executed) != 0 {
		t.Fatalf("write tool ran before approval: response=%#v executed=%#v", first, executor.executed)
	}
	final, err := engine.ResolveApproval(t.Context(), first.SessionID, first.Approval.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !final.Done || final.Reply != "修复完成。" || len(executor.executed) != 1 || executor.executed[0] != "write_repair" {
		t.Fatalf("approved tool was not completed: response=%#v executed=%#v", final, executor.executed)
	}
}

func TestEngineDeniesWriteToolWithoutExecution(t *testing.T) {
	engine, executor := newScriptedEngine(t, modelToolCall("write_repair"), modelReply("已取消。"))
	first, err := engine.Turn(t.Context(), "", "修复它")
	if err != nil {
		t.Fatal(err)
	}
	final, err := engine.ResolveApproval(t.Context(), first.SessionID, first.Approval.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if !final.Done || final.Reply != "已取消。" || len(executor.executed) != 0 {
		t.Fatalf("denied tool should not execute: response=%#v executed=%#v", final, executor.executed)
	}
}

func TestEngineReportsExecutedWriteWhenModelSummaryFails(t *testing.T) {
	engine, executor := newScriptedEngine(t, modelToolCall("write_repair"), `{"not":"a model response"}`)
	first, err := engine.Turn(t.Context(), "", "修复它")
	if err != nil {
		t.Fatal(err)
	}
	final, err := engine.ResolveApproval(t.Context(), first.SessionID, first.Approval.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !final.Done || !strings.Contains(final.Reply, "操作已经执行") || len(executor.executed) != 1 {
		t.Fatalf("executed write was not reported safely: response=%#v executed=%#v", final, executor.executed)
	}
}

func TestEngineLimitsToolRounds(t *testing.T) {
	responses := make([]string, maxAgentRounds)
	for i := range responses {
		responses[i] = modelToolCall("read_status")
	}
	engine, executor := newScriptedEngine(t, responses...)
	if _, err := engine.Turn(t.Context(), "", "循环检查"); err == nil || !strings.Contains(err.Error(), "安全轮次") {
		t.Fatalf("expected tool round limit, got %v", err)
	}
	if len(executor.executed) != maxAgentRounds {
		t.Fatalf("unexpected execution count: %d", len(executor.executed))
	}
	if len(engine.sessions) != 0 {
		t.Fatal("failed new session should be discarded")
	}
}

func TestEngineDoesNotEvictExistingSessionOnLookup(t *testing.T) {
	engine, _ := newScriptedEngine(t)
	ids := make([]string, 0, maxSessions)
	for range maxSessions {
		id, _, err := engine.session("")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if _, _, err := engine.session(ids[0]); err != nil {
		t.Fatalf("lookup evicted an existing session: %v", err)
	}
	if len(engine.sessions) != maxSessions {
		t.Fatalf("lookup changed session count: %d", len(engine.sessions))
	}
}

func TestToolResultTruncationPreservesUTF8(t *testing.T) {
	executor := &testToolExecutor{}
	engine := &Engine{executor: executor}
	large := strings.Repeat("好", maxToolResultBytes)
	executorResult := &fixedResultExecutor{result: large}
	engine.executor = executorResult
	result, err := engine.executeTool(t.Context(), ToolCall{Function: FunctionCall{Name: "read_status", Arguments: "{}"}})
	if err != nil || !strings.Contains(result, "结果已由 LongHub 截断") || !utf8.ValidString(result) {
		t.Fatalf("invalid truncated result: err=%v", err)
	}
}

func TestToolResultRedactsCredentialsAndUserHome(t *testing.T) {
	engine := &Engine{executor: &fixedResultExecutor{result: `C:\Users\Alice\.openclaw token=secret-value`}}
	result, err := engine.executeTool(t.Context(), ToolCall{Function: FunctionCall{Name: "read_status", Arguments: "{}"}})
	if err != nil || strings.Contains(result, "Alice") || strings.Contains(result, "secret-value") || !strings.Contains(result, "[REDACTED]") {
		t.Fatalf("tool result was not redacted: %q err=%v", result, err)
	}
}

func TestConnectionUsesModelWithoutExecutingTools(t *testing.T) {
	engine, executor := newScriptedEngine(t, `{"choices":[{"message":{"role":"assistant","content":"连接正常"}}]}`)
	if err := engine.TestConnection(t.Context()); err != nil {
		t.Fatal(err)
	}
	executor.mu.Lock()
	defer executor.mu.Unlock()
	if len(executor.executed) != 0 {
		t.Fatalf("connection test executed tools: %#v", executor.executed)
	}
}

func TestPublicErrorMessageRedactsCredentialsAndHome(t *testing.T) {
	message := PublicErrorMessage(errors.New(`provider rejected token=secret-value at C:\Users\alice\config`))
	if strings.Contains(message, "secret-value") || strings.Contains(strings.ToLower(message), `c:\users\alice`) {
		t.Fatalf("public error leaked sensitive text: %q", message)
	}
}

type fixedResultExecutor struct{ result string }

func (e *fixedResultExecutor) Specs() []ToolSpec { return nil }
func (e *fixedResultExecutor) Execute(context.Context, string, json.RawMessage) (string, error) {
	return e.result, nil
}

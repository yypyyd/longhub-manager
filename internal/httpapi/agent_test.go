package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yypyyd/longhub-manager/internal/configbackup"
	"github.com/yypyyd/longhub-manager/internal/manageragent"
	"github.com/yypyyd/longhub-manager/internal/runtime"
)

type httpTestSecret struct{ value string }

func (s *httpTestSecret) Get() (string, error) { return s.value, nil }
func (s *httpTestSecret) Set(value string) error {
	s.value = value
	return nil
}
func (s *httpTestSecret) Delete() error { s.value = ""; return nil }

func authenticatedRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestServerSetsDesktopSecurityHeaders(t *testing.T) {
	server := NewServer(runtime.NewNativeAdapter(skillsControlRunner{}), "test-token")
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	response := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected root response: %d", response.Code)
	}
	for header, expected := range map[string]string{
		"Cache-Control": "no-store", "X-Content-Type-Options": "nosniff",
		"X-Frame-Options": "DENY", "Referrer-Policy": "no-referrer",
	} {
		if response.Header().Get(header) != expected {
			t.Errorf("missing security header %s=%q", header, expected)
		}
	}
	if !strings.Contains(response.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Error("content security policy does not block framing")
	}
}

func TestManagerAgentConfigAndApprovalFlow(t *testing.T) {
	var modelMu sync.Mutex
	modelResponses := []string{
		`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call-backup","type":"function","function":{"name":"create_config_backup","arguments":"{}"}}]}}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"备份已经创建。"}}]}`,
	}
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer model-secret" {
			t.Error("model API key was not sent in Authorization header")
		}
		modelMu.Lock()
		defer modelMu.Unlock()
		if len(modelResponses) == 0 {
			http.Error(w, "unexpected request", http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, modelResponses[0])
		modelResponses = modelResponses[1:]
	}))
	defer modelServer.Close()

	root := t.TempDir()
	agentConfig, err := manageragent.NewConfigStore(filepath.Join(root, "agent.json"), &httpTestSecret{})
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "openclaw.json")
	if err := os.WriteFile(configPath, []byte(`{"gateway":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	backups, err := configbackup.New(configPath, filepath.Join(root, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	server := NewServerWithOptions(runtime.NewNativeAdapter(skillsControlRunner{}), "test-token", ServerOptions{
		ConfigBackups: backups,
		AgentConfig:   agentConfig,
		AgentModel:    manageragent.NewModelClient(modelServer.Client()),
	})

	res := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(res, authenticatedRequest(http.MethodPut, "/api/v1/agent/config", `{"base_url":"`+modelServer.URL+`","model":"manager-model","api_key":"model-secret"}`))
	if res.Code != http.StatusOK || strings.Contains(res.Body.String(), "model-secret") || !strings.Contains(res.Body.String(), `"protocol":"auto"`) {
		t.Fatalf("unexpected config response %d: %s", res.Code, res.Body.String())
	}

	res = httptest.NewRecorder()
	server.http.Handler.ServeHTTP(res, authenticatedRequest(http.MethodPost, "/api/v1/agent/turn", `{"message":"请创建配置备份"}`))
	if res.Code != http.StatusOK {
		t.Fatalf("unexpected turn response %d: %s", res.Code, res.Body.String())
	}
	var turn manageragent.TurnResponse
	if err := json.Unmarshal(res.Body.Bytes(), &turn); err != nil {
		t.Fatal(err)
	}
	if turn.Approval == nil || turn.Done {
		t.Fatalf("write tool did not pause for approval: %#v", turn)
	}
	items, _ := backups.List()
	if len(items) != 0 {
		t.Fatal("backup was created before user approval")
	}

	res = httptest.NewRecorder()
	body := `{"session_id":"` + turn.SessionID + `","approval_id":"` + turn.Approval.ID + `","approved":true}`
	server.http.Handler.ServeHTTP(res, authenticatedRequest(http.MethodPost, "/api/v1/agent/approval", body))
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "备份已经创建") {
		t.Fatalf("unexpected approval response %d: %s", res.Code, res.Body.String())
	}
	items, err = backups.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("approved backup was not created: count=%d err=%v", len(items), err)
	}
}

func TestManagerAgentConnectionTestReturnsRedactedProviderError(t *testing.T) {
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"message":"token model-secret has no channel for requested model"}}`)
	}))
	defer modelServer.Close()

	root := t.TempDir()
	agentConfig, err := manageragent.NewConfigStore(filepath.Join(root, "agent.json"), &httpTestSecret{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agentConfig.Save(modelServer.URL+"/v1", "unavailable-model", "model-secret"); err != nil {
		t.Fatal(err)
	}
	server := NewServerWithOptions(runtime.NewNativeAdapter(skillsControlRunner{}), "test-token", ServerOptions{
		AgentConfig: agentConfig,
		AgentModel:  manageragent.NewModelClient(modelServer.Client()),
	})

	res := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(res, authenticatedRequest(http.MethodPost, "/api/v1/agent/config/test", ""))
	if res.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(res.Body.String(), "no channel for requested model") ||
		strings.Contains(res.Body.String(), "model-secret") {
		t.Fatalf("unexpected connection test response %d: %s", res.Code, res.Body.String())
	}

	res = httptest.NewRecorder()
	server.http.Handler.ServeHTTP(res, authenticatedRequest(http.MethodPost, "/api/v1/agent/turn", `{"message":"检查"}`))
	if res.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(res.Body.String(), "no channel for requested model") ||
		strings.Contains(res.Body.String(), "model-secret") {
		t.Fatalf("unexpected turn error response %d: %s", res.Code, res.Body.String())
	}
}

func TestManagerAgentTurnStreamFlushesLifecycleAndReplyDeltas(t *testing.T) {
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"检查完成"}}]}`)
	}))
	defer modelServer.Close()

	root := t.TempDir()
	agentConfig, err := manageragent.NewConfigStore(filepath.Join(root, "agent.json"), &httpTestSecret{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agentConfig.Save(modelServer.URL, "manager-model", "model-secret"); err != nil {
		t.Fatal(err)
	}
	server := NewServerWithOptions(runtime.NewNativeAdapter(skillsControlRunner{}), "test-token", ServerOptions{
		AgentConfig: agentConfig,
		AgentModel:  manageragent.NewModelClient(modelServer.Client()),
	})
	res := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(res, authenticatedRequest(http.MethodPost, "/api/v1/agent/turn/stream", `{"message":"检查"}`))
	if res.Code != http.StatusOK || !strings.HasPrefix(res.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("unexpected stream response %d %q: %s", res.Code, res.Header().Get("Content-Type"), res.Body.String())
	}
	body := res.Body.String()
	wantOrder := []string{`"type":"turn_started"`, `"type":"model_started"`, `"type":"answer_started"`, `"type":"reply_delta"`, `"type":"done"`}
	position := -1
	for _, marker := range wantOrder {
		next := strings.Index(body[position+1:], marker)
		if next < 0 {
			t.Fatalf("stream is missing %q: %s", marker, body)
		}
		position += next + 1
	}
	if strings.Contains(body, `"reply":"检查完成"`) || !strings.Contains(body, `"delta":"检"`) {
		t.Fatalf("reply was not delivered exclusively as deltas: %s", body)
	}
}

type inventoryRunner struct{}

func (inventoryRunner) LookPath(string) (string, error) { return "openclaw", nil }
func (inventoryRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return []byte(`{"models":[{"id":"demo","apiKey":"hidden"}]}`), nil
}

func TestInventoryEndpointReturnsStructuredRedactedJSON(t *testing.T) {
	server := NewServer(runtime.NewNativeAdapter(inventoryRunner{}), "test-token")
	res := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(res, authenticatedRequest(http.MethodGet, "/api/v1/inventory/models", ""))
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"id":"demo"`) || strings.Contains(res.Body.String(), "hidden") {
		t.Fatalf("unexpected inventory response %d: %s", res.Code, res.Body.String())
	}
}

type failingRepairRunner struct{ configPath string }

func (f failingRepairRunner) LookPath(string) (string, error) { return "openclaw", nil }
func (f failingRepairRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "doctor" {
		if err := os.WriteFile(f.configPath, []byte(`{"broken":true}`), 0600); err != nil {
			return nil, err
		}
		return []byte("repair changed config"), nil
	}
	return []byte(`{"ok":true}`), nil
}
func (f failingRepairRunner) RunWithEnv(_ context.Context, env []string, _ string, _ ...string) ([]byte, error) {
	if len(env) != 1 {
		return nil, errors.New("missing candidate path")
	}
	path := strings.TrimPrefix(env[0], "OPENCLAW_CONFIG_PATH=")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.Contains(string(data), "broken") {
		return nil, errors.New("invalid config")
	}
	return []byte(`{"ok":true}`), nil
}

func TestRepairTransactionRollsBackInvalidResult(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "openclaw.json")
	original := `{"before":true}`
	if err := os.WriteFile(configPath, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	backups, err := configbackup.New(configPath, filepath.Join(root, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	server := NewServerWithOptions(runtime.NewNativeAdapter(failingRepairRunner{configPath: configPath}), "test-token", ServerOptions{ConfigBackups: backups})
	res := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(res, authenticatedRequest(http.MethodPost, "/api/v1/runtime/control", `{"action":"repair","confirm":true}`))
	if res.Code != http.StatusUnprocessableEntity || !strings.Contains(res.Body.String(), "OPENCLAW_REPAIR_VALIDATION_FAILED") || !strings.Contains(res.Body.String(), `"rolled_back":true`) {
		t.Fatalf("unexpected repair response %d: %s", res.Code, res.Body.String())
	}
	data, err := os.ReadFile(configPath)
	if err != nil || string(data) != original {
		t.Fatalf("repair transaction did not restore config: data=%q err=%v", data, err)
	}
}

func TestAgentWebUIStreamsRealLifecycleEvents(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	for _, marker := range []string{
		`"message assistant agent-working"`,
		`row.setAttribute("role", "status")`,
		`@keyframes agent-spin`,
		`@keyframes agent-progress-slide`,
		`formatAgentElapsed`,
		`streamAgentRequest`,
		`case "tool_started"`,
		`case "tool_completed"`,
		`case "reply_delta"`,
		`class="composer-prompts"`,
		`class="view assistant-view"`,
		`el("details", "agent-working-details"`,
		`.assistant-view.active { display: flex; height: 100%; min-height: 0; }`,
		`overflow-y: auto; overscroll-behavior: contain;`,
		`--agent-message-max: 1200px`,
		`width: min(var(--agent-message-max), 100%); max-width: 100%;`,
	} {
		if !strings.Contains(page, marker) {
			t.Fatalf("agent activity UI is missing %q", marker)
		}
	}
	start := strings.Index(page, "async function sendAgent(message)")
	end := strings.Index(page, "async function resolveApproval(approved)")
	if start < 0 || end <= start {
		t.Fatal("agent turn function boundary is missing")
	}
	turn := page[start:end]
	activity := strings.Index(turn, `startAgentActivity("正在连接管家引擎…")`)
	request := strings.Index(turn, `await streamAgentRequest("/api/v1/agent/turn/stream"`)
	if activity < 0 || request <= activity {
		t.Fatalf("agent activity must be visible before the streaming turn request")
	}
}

func TestInstallWebUIUsesPreflightGatedWorkflow(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	for _, marker := range []string{
		`id="install-current-version"`,
		`id="install-target-version"`,
		`id="install-latest-version"`,
		`id="install-node-version"`,
		`id="install" disabled`,
		`async function loadInstallWorkspace()`,
		`call("/api/v1/runtime/latest-version")`,
		`function compareOpenClawVersions(leftValue, rightValue)`,
		`label: "升级到 " + targetVersion`,
		`label: "已是最新审核版本"`,
		`发现新版 · 等待 LongHub 审核`,
		`installPreflightReady = Boolean(report.ready)`,
		`installBusy || !installPreflightReady || !action.allowed`,
		`setInstallStep(3, "success"`,
	} {
		if !strings.Contains(page, marker) {
			t.Fatalf("guided install UI is missing %q", marker)
		}
	}
	if strings.Contains(page, "重新安装审核版本") {
		t.Fatal("guided install UI must not present reinstall as the primary action")
	}
}

func TestServicesWebUILoadsGatewayStateOnEntry(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	for _, marker := range []string{
		`id="services-status"`,
		`id="services-refresh"`,
		`if (page === "services") loadServicesWorkspace()`,
		`async function loadServicesWorkspace(clearOperation = true)`,
		`JSON.stringify({ action: "status", confirm: false })`,
		`JSON.stringify({ action: "task-status", confirm: false })`,
		`function renderServicesStatus(gatewayError = null, taskError = null)`,
		`function syncServiceControls()`,
		`正在读取 Gateway 和 Windows 自动启动状态`,
	} {
		if !strings.Contains(page, marker) {
			t.Fatalf("services initial status UI is missing %q", marker)
		}
	}
	if strings.Contains(page, `<div class="operation data-state" id="services-operation"><div class="empty">选择操作以查看结果。</div></div>`) {
		t.Fatal("services page must not start with an empty action placeholder")
	}
}

func TestDashboardWebUIRecoversFromLaunchTimeout(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	for _, marker := range []string{
		`async function callWithTimeout(path, options, timeoutMs, timeoutMessage)`,
		`action === "dashboard" ? "正在创建 OpenClaw 命令行窗口…"`,
		`await callWithTimeout("/api/v1/runtime/control", request, 12000`,
		`if (error && error.name === "AbortError") throw new Error(timeoutMessage)`,
		`OpenClaw 命令行窗口已创建`,
		`if (button) button.disabled = false`,
	} {
		if !strings.Contains(page, marker) {
			t.Fatalf("dashboard timeout recovery UI is missing %q", marker)
		}
	}
}

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

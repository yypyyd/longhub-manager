package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yypyyd/longhub-manager/internal/configbackup"
	"github.com/yypyyd/longhub-manager/internal/runtime"
)

type manageRunner struct {
	lastArgs []string
	runCalls int
}

type invalidConfigMutationRunner struct{ configPath string }

func (r invalidConfigMutationRunner) LookPath(string) (string, error) { return "openclaw", nil }
func (r invalidConfigMutationRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if len(args) > 1 && args[0] == "models" && args[1] == "set" {
		if err := os.WriteFile(r.configPath, []byte(`{"broken":true}`), 0600); err != nil {
			return nil, err
		}
	}
	return []byte(`{"ok":true}`), nil
}
func (r invalidConfigMutationRunner) RunWithEnv(_ context.Context, env []string, _ string, _ ...string) ([]byte, error) {
	path := strings.TrimPrefix(env[0], "OPENCLAW_CONFIG_PATH=")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.Contains(string(data), "broken") {
		return nil, errors.New("invalid config")
	}
	return []byte(`{"valid":true}`), nil
}

func (r *manageRunner) LookPath(string) (string, error) { return "openclaw", nil }
func (r *manageRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.lastArgs = append([]string(nil), args...)
	r.runCalls++
	return []byte(`{"ok":true}`), nil
}
func (r *manageRunner) RunWithEnv(context.Context, []string, string, ...string) ([]byte, error) {
	return []byte(`{"valid":true}`), nil
}

func newManagementTestServer(t *testing.T, runner runtime.CommandRunner) (*Server, *configbackup.Manager) {
	t.Helper()
	root := t.TempDir()
	configPath := filepath.Join(root, "openclaw.json")
	if err := os.WriteFile(configPath, []byte(`{"gateway":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	backups, err := configbackup.New(configPath, filepath.Join(root, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	server := NewServerWithOptions(runtime.NewNativeAdapter(runner), "test-token", ServerOptions{ConfigBackups: backups})
	return server, backups
}

func TestManagementAPIRequiresConfirmationBeforeCommand(t *testing.T) {
	runner := &manageRunner{}
	server, _ := newManagementTestServer(t, runner)
	res := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(res, authenticatedRequest(
		http.MethodPost, "/api/v1/manage",
		`{"resource":"models","action":"set_default","model":"openai/gpt-5","confirm":false}`,
	))
	if res.Code != http.StatusPreconditionRequired || runner.runCalls != 0 {
		t.Fatalf("unconfirmed management reached runner: status=%d calls=%d", res.Code, runner.runCalls)
	}
}

func TestManagementAPIBacksUpAndValidatesConfigMutation(t *testing.T) {
	runner := &manageRunner{}
	server, backups := newManagementTestServer(t, runner)
	res := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(res, authenticatedRequest(
		http.MethodPost, "/api/v1/manage",
		`{"resource":"models","action":"set_default","model":"openai/gpt-5","confirm":true}`,
	))
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"validated":true`) {
		t.Fatalf("unexpected management response %d: %s", res.Code, res.Body.String())
	}
	if !equalStrings(runner.lastArgs, []string{"models", "set", "openai/gpt-5"}) {
		t.Fatalf("unexpected management args: %v", runner.lastArgs)
	}
	items, err := backups.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("expected a recovery checkpoint: count=%d err=%v", len(items), err)
	}
}

func TestManagementAPIUsesTypedCronPayload(t *testing.T) {
	runner := &manageRunner{}
	server, _ := newManagementTestServer(t, runner)
	res := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(res, authenticatedRequest(
		http.MethodPost, "/api/v1/manage",
		`{"resource":"cron","action":"add","name":"daily","message":"检查状态","schedule_type":"every","schedule":"1h","agent_id":"main","confirm":true}`,
	))
	if res.Code != http.StatusOK {
		t.Fatalf("unexpected cron response %d: %s", res.Code, res.Body.String())
	}
	want := []string{
		"cron", "add", "--name=daily", "--message=检查状态", "--session=isolated",
		"--every=1h", "--json", "--agent=main",
	}
	if !equalStrings(runner.lastArgs, want) {
		t.Fatalf("unexpected cron args: %v", runner.lastArgs)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestManagementAPIRejectsUnknownFields(t *testing.T) {
	runner := &manageRunner{}
	server, _ := newManagementTestServer(t, runner)
	res := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(res, authenticatedRequest(
		http.MethodPost, "/api/v1/manage",
		`{"resource":"models","action":"set_default","model":"openai/gpt-5","command":"whoami","confirm":true}`,
	))
	if res.Code != http.StatusBadRequest || runner.runCalls != 0 {
		t.Fatalf("unknown field was accepted: status=%d calls=%d", res.Code, runner.runCalls)
	}
}

func TestManagementAPIRollsBackInvalidConfigMutation(t *testing.T) {
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
	server := NewServerWithOptions(
		runtime.NewNativeAdapter(invalidConfigMutationRunner{configPath: configPath}),
		"test-token", ServerOptions{ConfigBackups: backups},
	)
	response := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(response, authenticatedRequest(
		http.MethodPost, "/api/v1/manage",
		`{"resource":"models","action":"set_default","model":"openai/gpt-5","confirm":true}`,
	))
	if response.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(response.Body.String(), "OPENCLAW_CONFIG_MUTATION_VALIDATION_FAILED") ||
		!strings.Contains(response.Body.String(), `"rolled_back":true`) {
		t.Fatalf("unexpected invalid mutation response %d: %s", response.Code, response.Body.String())
	}
	data, err := os.ReadFile(configPath)
	if err != nil || string(data) != original {
		t.Fatalf("invalid config mutation was not rolled back: data=%q err=%v", data, err)
	}
}

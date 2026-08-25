package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yypyyd/longhub-manager/internal/configbackup"
	"github.com/yypyyd/longhub-manager/internal/runtime"
)

type skillsControlRunner struct{}

func (skillsControlRunner) LookPath(string) (string, error) { return "openclaw", nil }

func (skillsControlRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return []byte("技能 (1/1 ready)\n| Status | Skill | Description | Source |\n| ✓ ready | sample | Works. | local |"), nil
}

func (skillsControlRunner) RunWithEnv(context.Context, []string, string, ...string) ([]byte, error) {
	return []byte(`{"ok":true}`), nil
}

func TestSkillsControlReturnsStructuredRecords(t *testing.T) {
	server := NewServer(runtime.NewNativeAdapter(skillsControlRunner{}), "test-token")
	requestBody := strings.NewReader(`{"action":"skills","confirm":false}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/control", requestBody)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", res.Code, res.Body.String())
	}
	var body struct {
		Action  string                `json:"action"`
		Summary runtime.SkillSummary  `json:"summary"`
		Skills  []runtime.SkillRecord `json:"skills"`
		Output  string                `json:"output"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Action != "skills" || body.Summary.Ready != 1 || len(body.Skills) != 1 || body.Skills[0].Name != "sample" {
		t.Fatalf("unexpected response: %+v", body)
	}
	if body.Output != "" {
		t.Fatalf("raw CLI output must not be returned by the Skills API: %q", body.Output)
	}
}

func TestReadOnlyConsoleActionsAreConnected(t *testing.T) {
	server := NewServer(runtime.NewNativeAdapter(skillsControlRunner{}), "test-token")
	for _, action := range []string{"models", "agents", "channels", "cron"} {
		t.Run(action, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/control", strings.NewReader(`{"action":"`+action+`","confirm":false}`))
			req.RemoteAddr = "127.0.0.1:12345"
			req.Header.Set("Authorization", "Bearer test-token")
			res := httptest.NewRecorder()
			server.http.Handler.ServeHTTP(res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("unexpected status %d: %s", res.Code, res.Body.String())
			}
		})
	}
}

func TestDashboardActionDoesNotReturnOpenClawURL(t *testing.T) {
	server := NewServer(runtime.NewNativeAdapter(skillsControlRunner{}), "test-token")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/control", strings.NewReader(`{"action":"dashboard","confirm":false}`))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "openclaw") || strings.Contains(res.Body.String(), "token") {
		t.Fatalf("dashboard response must not expose CLI output: %s", res.Body.String())
	}
}

func TestRepairCreatesBackupBeforeRunningFixedRepair(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "openclaw.json")
	if err := os.WriteFile(configPath, []byte(`{"gateway":{"port":18789}}`), 0600); err != nil {
		t.Fatal(err)
	}
	backups, err := configbackup.New(configPath, filepath.Join(directory, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	server := NewServerWithOptions(runtime.NewNativeAdapter(skillsControlRunner{}), "test-token", ServerOptions{ConfigBackups: backups})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/control", strings.NewReader(`{"action":"repair","confirm":true}`))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", res.Code, res.Body.String())
	}
	items, err := backups.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one safety backup, got %d (%v)", len(items), err)
	}
}

package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yypyyd/longhub-manager/internal/runtime"
)

type versionAPIRunner struct {
	name  string
	args  []string
	calls int
	out   string
	err   error
}

func (r *versionAPIRunner) LookPath(file string) (string, error) {
	if file != "npm" {
		return "", errors.New("not found")
	}
	return `C:\tools\npm.cmd`, nil
}

func (r *versionAPIRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls++
	r.name = name
	r.args = append([]string(nil), args...)
	return []byte(r.out), r.err
}

func TestLatestVersionAPIIsAuthenticatedReadOnlyMetadata(t *testing.T) {
	runner := &versionAPIRunner{out: `"2026.8.3"`}
	server := NewServer(runtime.NewNativeAdapter(runner), "test-token")

	unauthorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/latest-version", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	server.http.Handler.ServeHTTP(unauthorized, request)
	if unauthorized.Code != http.StatusUnauthorized || runner.calls != 0 {
		t.Fatalf("unauthorized lookup reached npm: status=%d calls=%d", unauthorized.Code, runner.calls)
	}

	wrongMethod := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(wrongMethod, authenticatedRequest(http.MethodPost, "/api/v1/runtime/latest-version", `{}`))
	if wrongMethod.Code != http.StatusMethodNotAllowed || runner.calls != 0 {
		t.Fatalf("non-GET lookup reached npm: status=%d calls=%d", wrongMethod.Code, runner.calls)
	}

	response := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/api/v1/runtime/latest-version", ""))
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `"latest_version":"2026.8.3"`) || !strings.Contains(body, `"reviewed_version":"2026.7.1-2"`) {
		t.Fatalf("unexpected version response %d: %s", response.Code, body)
	}
	if runner.calls != 1 || runner.name != `C:\tools\npm.cmd` || !equalStrings(runner.args, []string{"view", "openclaw", "version", "--json"}) {
		t.Fatalf("unexpected npm lookup: calls=%d name=%q args=%v", runner.calls, runner.name, runner.args)
	}
	if strings.Contains(body, `C:\tools`) || strings.Contains(body, `"command"`) || strings.Contains(body, `"args"`) {
		t.Fatalf("version response exposed command metadata: %s", body)
	}
}

func TestLatestVersionAPIRedactsRegistryFailure(t *testing.T) {
	runner := &versionAPIRunner{out: "private registry output", err: errors.New("private runner error")}
	server := NewServer(runtime.NewNativeAdapter(runner), "test-token")
	response := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/api/v1/runtime/latest-version", ""))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "OPENCLAW_VERSION_CHECK_UNAVAILABLE") {
		t.Fatalf("unexpected failure response %d: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "private") {
		t.Fatalf("version API exposed registry failure: %s", response.Body.String())
	}
}

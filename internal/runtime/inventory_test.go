package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestInventoryUsesFixedJSONCommandAndRedactsSecrets(t *testing.T) {
	runner := &fakeRunner{
		path:   "openclaw",
		output: `{"items":[{"name":"demo","api_key":"secret","nested":{"access-token":"token-value","safe":"visible"},"endpoint":"https://example.test/path?token=url-secret","path":"C:\\Users\\Alice\\.openclaw","message":"Bearer header-secret"}]}`,
	}
	data, err := NewNativeAdapter(runner).Inventory(context.Background(), "models")
	if err != nil {
		t.Fatal(err)
	}
	if !slicesEqual(runner.lastArgs, []string{"models", "list", "--json"}) {
		t.Fatalf("unexpected inventory command: %v", runner.lastArgs)
	}
	encoded := string(data)
	if strings.Contains(encoded, "url-secret") || strings.Contains(encoded, "header-secret") ||
		strings.Contains(encoded, "token-value") || strings.Contains(encoded, "Alice") ||
		!strings.Contains(encoded, "visible") || !strings.Contains(encoded, "~") || !strings.Contains(encoded, ".openclaw") ||
		!strings.Contains(encoded, "%5BREDACTED%5D") || strings.Count(encoded, "[REDACTED]") < 3 {
		t.Fatalf("unexpected redacted inventory: %s", encoded)
	}
}

func TestInventoryRejectsUnknownKindAndInvalidJSON(t *testing.T) {
	runner := &fakeRunner{path: "openclaw", output: "not-json"}
	adapter := NewNativeAdapter(runner)
	if _, err := adapter.Inventory(context.Background(), "../../shell"); err == nil {
		t.Fatal("unknown inventory kind was accepted")
	}
	if _, err := adapter.Inventory(context.Background(), "models"); err == nil {
		t.Fatal("invalid JSON was accepted")
	}
}

func TestInventoryAcceptsStructuredFindingsWithNonzeroExit(t *testing.T) {
	runner := &fakeRunner{path: "openclaw", output: `{"valid":false,"findings":["configuration issue"]}`, runErr: errors.New("exit status 1")}
	data, err := NewNativeAdapter(runner).Inventory(context.Background(), "diagnostics")
	if err != nil || !strings.Contains(string(data), "configuration issue") {
		t.Fatalf("structured diagnostic findings were discarded: data=%s err=%v", data, err)
	}
}

package main

import (
	"errors"
	"testing"

	managerRuntime "github.com/yypyyd/longhub-manager/internal/runtime"
)

func TestParseManagerStartupModeIsFixed(t *testing.T) {
	tests := []struct {
		args  []string
		want  managerStartupMode
		valid bool
	}{
		{valid: true, want: managerStartupInteractive},
		{args: []string{managerRuntime.LongHubGatewayTaskArguments}, valid: true, want: managerStartupGateway},
		{args: []string{"--remove-autostart"}, valid: true, want: managerStartupRemoveAutostart},
		{args: []string{"--port", "1234"}},
	}
	for _, test := range tests {
		got, err := parseManagerStartupMode(test.args)
		if (err == nil) != test.valid || (test.valid && got != test.want) {
			t.Fatalf("args=%v got=%v err=%v", test.args, got, err)
		}
	}
}

func TestNewManagerAgentConfigStoreRequiresAbsoluteConfigDirectory(t *testing.T) {
	if _, err := newManagerAgentConfigStore(nil); err == nil {
		t.Fatal("nil user config resolver was accepted")
	}
	if _, err := newManagerAgentConfigStore(func() (string, error) { return "relative", nil }); err == nil {
		t.Fatal("relative user config directory was accepted")
	}
	if _, err := newManagerAgentConfigStore(func() (string, error) { return "", errors.New("unavailable") }); err == nil {
		t.Fatal("resolver error was accepted")
	}
	if _, err := newManagerAgentConfigStore(func() (string, error) { return t.TempDir(), nil }); err != nil {
		t.Fatalf("absolute user config directory was rejected: %v", err)
	}
}

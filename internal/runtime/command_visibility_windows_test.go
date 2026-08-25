//go:build windows

package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigureBackgroundCommandHidesConsole(t *testing.T) {
	command := exec.Command("openclaw")
	configureBackgroundCommand(command)
	if command.SysProcAttr == nil || !command.SysProcAttr.HideWindow {
		t.Fatal("background command must hide its console window")
	}
	if command.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatal("background command must set CREATE_NO_WINDOW")
	}
}

func TestResolveWindowsShimCommandUsesNodeEntryPoint(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "node_modules", "@qingchencloud", "openclaw-zh")
	if err := os.MkdirAll(script, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node.exe"), []byte("stub"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(script, "openclaw.mjs"), []byte("stub"), 0o600); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(dir, "openclaw.cmd")
	name, args := resolveWindowsShimCommand(shim, []string{"gateway", "status"})
	if name != filepath.Join(dir, "node.exe") {
		t.Fatalf("name=%q", name)
	}
	want := []string{filepath.Join(script, "openclaw.mjs"), "gateway", "status"}
	if len(args) != len(want) {
		t.Fatalf("args=%q", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d]=%q want %q", i, args[i], want[i])
		}
	}
}

func TestPrepareDashboardConsoleCommandOpensPersistentWindow(t *testing.T) {
	openClaw := filepath.Join(t.TempDir(), "openclaw.cmd")
	if err := os.WriteFile(openClaw, []byte("@echo off\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command, err := prepareDashboardConsoleCommand(context.Background(), openClaw)
	if err != nil {
		t.Fatal(err)
	}
	if command.SysProcAttr == nil || command.SysProcAttr.HideWindow {
		t.Fatal("the dashboard console must be visible")
	}
	if command.SysProcAttr.CreationFlags&createNewConsole == 0 || command.SysProcAttr.CreationFlags&createNewProcessGroup == 0 {
		t.Fatal("dashboard must start in an independent console process group")
	}
	if command.SysProcAttr.CreationFlags&createNoWindow != 0 {
		t.Fatal("dashboard must not inherit background-command flags")
	}
	if command.SysProcAttr.CmdLine != dashboardConsoleCommandLine {
		t.Fatalf("unexpected dashboard command line: %q", command.SysProcAttr.CmdLine)
	}
	if !strings.Contains(dashboardConsoleScript, `call "%LONGHUB_OPENCLAW_DASHBOARD_COMMAND%" dashboard`) {
		t.Fatalf("dashboard console must stay open: %q", dashboardConsoleScript)
	}
	if strings.Contains(strings.ToLower(dashboardConsoleScript), "start ") {
		t.Fatal("dashboard must not rely on a synchronously waited START wrapper")
	}
	wantEnv := dashboardCommandEnvironment + "=" + openClaw
	found := false
	for _, entry := range command.Env {
		if strings.EqualFold(entry, wantEnv) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("dashboard command path missing from child environment: want %q", wantEnv)
	}
	if strings.Contains(dashboardConsoleScript, openClaw) {
		t.Fatal("dashboard command path must not be interpolated into cmd source")
	}
}

func TestPrepareDashboardConsoleCommandRejectsInvalidPath(t *testing.T) {
	if _, err := prepareDashboardConsoleCommand(context.Background(), "openclaw.cmd\r\nwhoami"); err == nil {
		t.Fatal("expected invalid dashboard command path to fail")
	}
}

//go:build windows

package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
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

//go:build windows

package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const createNoWindow = 0x08000000

// configureBackgroundCommand keeps Manager-owned CLI probes out of the user's
// desktop. HideWindow covers ordinary console applications; CREATE_NO_WINDOW
// also prevents cmd.exe/npm shims from creating a console during refresh.
func configureBackgroundCommand(command *exec.Cmd) {
	if command == nil {
		return
	}
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

// resolveWindowsShimCommand bypasses npm's .cmd shim for the OpenClaw CLI.
// The shim is a batch file that can ask the host shell/Windows Terminal to
// create a console even when the parent process requested CREATE_NO_WINDOW.
// Calling the Node entry point directly keeps status/health probes invisible
// and also makes their process tree deterministic.
func resolveWindowsShimCommand(name string, args []string) (string, []string) {
	base := strings.ToLower(filepath.Base(name))
	if base != "openclaw.cmd" && base != "openclaw.bat" {
		return name, args
	}
	dir := filepath.Dir(name)
	node := filepath.Join(dir, "node.exe")
	if nodeInfo, err := os.Stat(node); err != nil || nodeInfo.IsDir() {
		return name, args
	}
	var script string
	for _, candidate := range []string{
		filepath.Join(dir, "node_modules", "@qingchencloud", "openclaw-zh", "openclaw.mjs"),
		filepath.Join(dir, "node_modules", "openclaw", "openclaw.mjs"),
		filepath.Join(dir, "node_modules", "openclaw", "bin", "openclaw.mjs"),
	} {
		if scriptInfo, err := os.Stat(candidate); err == nil && !scriptInfo.IsDir() {
			script = candidate
			break
		}
	}
	if script == "" {
		return name, args
	}
	return node, append([]string{script}, args...)
}

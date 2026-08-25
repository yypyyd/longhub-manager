//go:build windows

package runtime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const createNoWindow = 0x08000000
const createNewConsole = 0x00000010
const createNewProcessGroup = 0x00000200

const dashboardCommandEnvironment = "LONGHUB_OPENCLAW_DASHBOARD_COMMAND"

const dashboardConsoleScript = `call "%LONGHUB_OPENCLAW_DASHBOARD_COMMAND%" dashboard`
const dashboardConsoleCommandLine = `/d /k ` + dashboardConsoleScript

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

// launchDashboardCommand is the one intentional exception to hidden Windows
// CLI execution. It creates a detached, visible console directly and returns
// as soon as Windows accepts the new process. The discovered executable path
// travels through the environment, not through command source, and the child
// can only receive the fixed "dashboard" subcommand from this launcher.
func launchDashboardCommand(ctx context.Context, name string) error {
	command, err := prepareDashboardConsoleCommand(ctx, name)
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func prepareDashboardConsoleCommand(ctx context.Context, name string) (*exec.Cmd, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "\x00\r\n\"") {
		return nil, errors.New("OpenClaw dashboard command path is invalid")
	}
	absName, err := filepath.Abs(name)
	if err != nil || !isRegularFile(absName) {
		return nil, errors.New("OpenClaw dashboard command is unavailable")
	}
	commandShell, err := exec.LookPath("cmd.exe")
	if err != nil {
		return nil, errors.New("Windows command shell is unavailable")
	}
	// Do not bind the detached console to the HTTP request context: returning
	// from the handler cancels that context and would immediately kill the
	// user-facing window. Context cancellation is checked above, before launch.
	command := exec.Command(commandShell)
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewConsole | createNewProcessGroup,
		// cmd.exe does not follow CommandLineToArgvW quoting. Supplying the
		// fixed command line directly avoids Go's generic argument escaping;
		// the discovered path remains isolated in the validated environment.
		CmdLine: dashboardConsoleCommandLine,
	}
	command.Env = mergeEnvironment(os.Environ(), []string{dashboardCommandEnvironment + "=" + absName})
	return command, nil
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

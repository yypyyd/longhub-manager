//go:build !windows

package runtime

import (
	"context"
	"os/exec"
)

func resolveWindowsShimCommand(name string, args []string) (string, []string) {
	return name, args
}

func configureBackgroundCommand(*exec.Cmd) {}

func launchDashboardCommand(ctx context.Context, name string) error {
	name, args := resolveWindowsShimCommand(name, []string{"dashboard"})
	command := exec.CommandContext(ctx, name, args...)
	_, err := runBoundedCommand(command, DefaultCommandOutputBytes)
	return err
}

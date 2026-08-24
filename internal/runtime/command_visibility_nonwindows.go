//go:build !windows

package runtime

import "os/exec"

func resolveWindowsShimCommand(name string, args []string) (string, []string) {
	return name, args
}

func configureBackgroundCommand(*exec.Cmd) {}

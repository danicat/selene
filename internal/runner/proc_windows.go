//go:build windows

package runner

import (
	"os/exec"
)

func configureProcessGroup(cmd *exec.Cmd) {
	// Windows processes are terminated via Process.Kill()
}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

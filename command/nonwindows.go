//go:build !windows
// +build !windows

package command

import (
	"os/exec"
	"syscall"
)

func setCommandProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

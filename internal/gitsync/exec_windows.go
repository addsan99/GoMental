package gitsync

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func configureGitCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

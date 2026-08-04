//go:build !windows

package gitsync

import "os/exec"

func configureGitCommand(_ *exec.Cmd) {}

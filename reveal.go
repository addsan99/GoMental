package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// revealFolder opens a directory in the operating system's file manager.
//
// Deliberately not a Service method: the HTTP viewer exposes the service to
// remote clients, and a remote request that pops a Finder window on someone
// else's desktop is not a feature. This stays in the desktop binding layer,
// where the caller is by construction the person sitting at the machine.
func revealFolder(path string) error {
	if path == "" {
		return errors.New("no folder to open")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	// Resolved paths are absolute, which also keeps a leading "-" from being
	// read as a flag by the launcher below.
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", abs)
	case "windows":
		cmd = exec.Command("explorer", abs)
	default:
		cmd = exec.Command("xdg-open", abs)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open the folder: %w", err)
	}
	// Explorer reports a non-zero status even when it opens the window, and the
	// others detach a GUI process we have no reason to wait on. Reap in the
	// background so the process table stays clean without blocking the UI.
	go func() { _ = cmd.Wait() }()
	return nil
}

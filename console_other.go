//go:build !windows

package main

// attachParentConsole is a no-op off Windows: a binary launched from a shell on
// Linux/macOS already has stdout/stderr wired to the terminal, so CLI output is
// visible without any special handling.
func attachParentConsole() {}

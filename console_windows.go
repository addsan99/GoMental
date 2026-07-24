//go:build windows

package main

import (
	"os"
	"syscall"
)

// attachParentConsole wires this process's standard streams to the console of
// whatever launched it. GoMental is built in the Windows GUI subsystem (so a
// double-click doesn't flash a console window), which means it starts with NO
// console attached — stdout/stderr go nowhere. Without this, running
// `GoMental.exe --help` (or `serve`/`mcp`) from a terminal produces no visible
// output at all, because the text is written to dead handles.
//
// AttachConsole(ATTACH_PARENT_PROCESS) borrows the launching terminal's console;
// we then re-open CONOUT$/CONIN$ so Go's std handles (captured as nil at start-up
// in a GUI process) point at that console. Best-effort: when there is no parent
// console (a GUI double-click), AttachConsole fails and this is a no-op, leaving
// the desktop launch path untouched.
//
// Windows caveat: because the process is GUI-subsystem, the shell does not wait
// for it, so the prompt returns before this output prints and the text lands just
// below the returned prompt. That is a cosmetic quirk of GUI-subsystem CLIs on
// Windows; the output is nonetheless shown.
func attachParentConsole() {
	const attachParentProcess = ^uintptr(0) // ATTACH_PARENT_PROCESS == (DWORD)-1
	attachConsole := syscall.NewLazyDLL("kernel32.dll").NewProc("AttachConsole")
	if ret, _, _ := attachConsole.Call(attachParentProcess); ret == 0 {
		return // no parent console (GUI launch), or already attached
	}
	// Re-open the console's output so fmt/log/os writes reach the terminal. Go
	// cached the (nil) std handles at start-up, before we had a console.
	if out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout = out
		os.Stderr = out
	}
	if in, err := os.OpenFile("CONIN$", os.O_RDONLY, 0); err == nil {
		os.Stdin = in
	}
}

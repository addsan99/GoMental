package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// startupT0 approximates process start — it is set during package initialisation,
// which runs a few milliseconds after the OS creates the process.
var startupT0 = time.Now()

var startupLogMu sync.Mutex

// startupLogEnabled reports whether launch timings should be recorded. The log is
// off by default (it was only needed to diagnose the WebView2 cold-start gap that
// the native splash now covers); set GOMENTAL_STARTUP_LOG=1 to re-enable it.
var startupLogEnabled = os.Getenv("GOMENTAL_STARTUP_LOG") != ""

// StartupLogPath is where launch timings are recorded.
func StartupLogPath() string {
	return filepath.Join(os.TempDir(), "gomental-startup.log")
}

// logStartup appends one timestamped line to the startup log so we can see where
// the pre-window launch time goes on the occasions the app takes several seconds
// to show anything.
//
// Each line carries two clocks:
//   - the absolute wall-clock time — compare the FIRST line of a launch to when
//     you actually double-clicked/ran it; a large gap there is time lost BEFORE
//     our process ran (antivirus/SmartScreen scan, OneDrive rehydration, cold
//     disk) which no splash can cover.
//   - the elapsed seconds since process start (+N.NNNs) — a large jump between
//     stages is time lost INSIDE startup (almost always WebView2 environment
//     initialisation), which a native pre-render splash could cover.
//
// Best-effort: any error is swallowed so instrumentation can never delay or break
// launch.
func logStartup(stage string) {
	if !startupLogEnabled {
		return
	}
	startupLogMu.Lock()
	defer startupLogMu.Unlock()
	f, err := os.OpenFile(StartupLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	now := time.Now()
	fmt.Fprintf(f, "%s  +%7.3fs  pid=%-6d  %s\n", now.Format("2006-01-02 15:04:05.000"), now.Sub(startupT0).Seconds(), os.Getpid(), stage)
}

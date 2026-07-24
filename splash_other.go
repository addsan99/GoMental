//go:build !windows

package main

// The native pre-render splash is Windows-only: it exists to cover the WebView2
// cold-start gap where Wails shows no window on Windows. On other platforms these
// are no-ops (and the in-webview #gm-splash in index.html covers the shorter gap
// there).
func showSplash()  {}
func closeSplash() {}

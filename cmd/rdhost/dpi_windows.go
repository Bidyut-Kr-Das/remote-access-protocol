//go:build windows

package main

import "golang.org/x/sys/windows"

// Opt into per-monitor DPI awareness so capture and input use physical pixels.
func init() {
	// -4 = DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2; failure (old Windows) is harmless.
	p := windows.NewLazySystemDLL("user32.dll").NewProc("SetProcessDpiAwarenessContext")
	if p.Find() == nil { // Call panics if the proc is missing
		p.Call(^uintptr(3))
	}
}

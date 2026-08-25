//go:build windows

package main

import "syscall"

var (
	dpiUser32                     = syscall.NewLazyDLL("user32.dll")
	dpiSetProcessAwarenessContext = dpiUser32.NewProc("SetProcessDpiAwarenessContext")
	dpiSetLegacyProcessDPIAware   = dpiUser32.NewProc("SetProcessDPIAware")
)

// enableHighDPI must run before any HWND is created. The release manifest
// provides the durable declaration, while this call also keeps locally built
// development binaries sharp when Windows display scaling is above 100%.
func enableHighDPI() {
	initializeDPI(
		func() bool {
			if dpiSetProcessAwarenessContext.Find() != nil {
				return false
			}
			// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 is the pseudo-handle -4.
			result, _, _ := dpiSetProcessAwarenessContext.Call(^uintptr(3))
			return result != 0
		},
		func() {
			if dpiSetLegacyProcessDPIAware.Find() == nil {
				_, _, _ = dpiSetLegacyProcessDPIAware.Call()
			}
		},
	)
}

func initializeDPI(setPerMonitorV2 func() bool, setLegacyAware func()) {
	if setPerMonitorV2 != nil && setPerMonitorV2() {
		return
	}
	if setLegacyAware != nil {
		setLegacyAware()
	}
}

//go:build windows

package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"

	webview2 "github.com/jchv/go-webview2"
)

const embeddedWindowShow = 5

var (
	embeddedUser32          = syscall.NewLazyDLL("user32.dll")
	embeddedShowWindow      = embeddedUser32.NewProc("ShowWindow")
	embeddedSetForeground   = embeddedUser32.NewProc("SetForegroundWindow")
	embeddedWindowMu        sync.Mutex
	embeddedWindow          webview2.WebView
	embeddedWindowLaunching bool
)

// openEmbeddedManagerWindow keeps the Manager UI inside the executable. The
// local HTTP server remains the backend, while WebView2 supplies the native
// Windows surface used by ClawPanel and other modern desktop apps.
func openEmbeddedManagerWindow(pageURL string) {
	if pageURL == "" {
		return
	}

	embeddedWindowMu.Lock()
	if embeddedWindow != nil {
		view := embeddedWindow
		embeddedWindowMu.Unlock()
		view.Dispatch(func() {
			showEmbeddedWindow(view)
			view.Navigate(pageURL)
		})
		return
	}
	if embeddedWindowLaunching {
		embeddedWindowMu.Unlock()
		return
	}
	embeddedWindowLaunching = true
	embeddedWindowMu.Unlock()

	go runEmbeddedManagerWindow(pageURL)
}

func runEmbeddedManagerWindow(pageURL string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	dataPath := embeddedWebViewDataPath()
	view := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		DataPath:  dataPath,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "LongHub Manager",
			Width:  1120,
			Height: 760,
			Center: true,
		},
	})
	if view == nil {
		embeddedWindowMu.Lock()
		embeddedWindowLaunching = false
		embeddedWindowMu.Unlock()
		log.Printf("WebView2 初始化失败，请安装 Microsoft Edge WebView2 Runtime")
		return
	}

	embeddedWindowMu.Lock()
	embeddedWindow = view
	embeddedWindowLaunching = false
	embeddedWindowMu.Unlock()

	view.SetSize(1120, 760, webview2.HintMin)
	view.Navigate(pageURL)
	showEmbeddedWindow(view)
	view.Run()

	embeddedWindowMu.Lock()
	if embeddedWindow == view {
		embeddedWindow = nil
	}
	embeddedWindowLaunching = false
	embeddedWindowMu.Unlock()
	view.Destroy()
}

func showEmbeddedWindow(view webview2.WebView) {
	if view == nil {
		return
	}
	hwnd := uintptr(view.Window())
	if hwnd == 0 {
		return
	}
	_, _, _ = embeddedShowWindow.Call(hwnd, embeddedWindowShow)
	_, _, _ = embeddedSetForeground.Call(hwnd)
}

func closeEmbeddedManagerWindow() {
	embeddedWindowMu.Lock()
	view := embeddedWindow
	embeddedWindowMu.Unlock()
	if view != nil {
		view.Dispatch(func() { view.Destroy() })
	}
}

func embeddedWebViewDataPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil || configDir == "" {
		return ""
	}
	path := filepath.Join(configDir, "LongHub", "webview2")
	if err := os.MkdirAll(path, 0o700); err != nil {
		return ""
	}
	return path
}

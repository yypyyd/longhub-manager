//go:build windows

package main

import "testing"

func TestActivateExistingManagerFailsClosedWithoutWindow(t *testing.T) {
	posted := false
	activated := activateExistingManagerWith(
		func(*uint16) uintptr { return 0 },
		func(uintptr, uint32) uintptr {
			posted = true
			return 1
		},
	)
	if activated || posted {
		t.Fatal("missing tray window unexpectedly activated")
	}
}

func TestActivateExistingManagerPostsDedicatedMessage(t *testing.T) {
	var gotWindow uintptr
	var gotMessage uint32
	activated := activateExistingManagerWith(
		func(*uint16) uintptr { return 42 },
		func(window uintptr, message uint32) uintptr {
			gotWindow = window
			gotMessage = message
			return 1
		},
	)
	if !activated || gotWindow != 42 || gotMessage != trayActivateMessage {
		t.Fatalf("activated=%t window=%d message=%d", activated, gotWindow, gotMessage)
	}
}

func TestActivateExistingManagerRequiresSuccessfulPost(t *testing.T) {
	activated := activateExistingManagerWith(
		func(*uint16) uintptr { return 42 },
		func(uintptr, uint32) uintptr { return 0 },
	)
	if activated {
		t.Fatal("failed window message unexpectedly activated")
	}
}

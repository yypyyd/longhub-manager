//go:build !windows

package main

import "context"

func activateExistingManager() bool { return false }

func closeEmbeddedManagerWindow() {}

func startPlatformTray(context.Context, context.CancelFunc, string, bool) error { return nil }

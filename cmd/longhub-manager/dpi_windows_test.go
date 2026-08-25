//go:build windows

package main

import (
	"os"
	"strings"
	"testing"
)

func TestInitializeDPIPrefersPerMonitorV2(t *testing.T) {
	legacyCalled := false
	initializeDPI(func() bool { return true }, func() { legacyCalled = true })
	if legacyCalled {
		t.Fatal("legacy DPI initialization ran after Per-Monitor V2 succeeded")
	}
}

func TestInitializeDPIFallsBackForOlderWindows(t *testing.T) {
	legacyCalled := false
	initializeDPI(func() bool { return false }, func() { legacyCalled = true })
	if !legacyCalled {
		t.Fatal("legacy DPI initialization did not run after Per-Monitor V2 failed")
	}
}

func TestReleaseManifestDeclaresPerMonitorV2(t *testing.T) {
	manifest, err := os.ReadFile("../../scripts/longhub-manager.manifest")
	if err != nil {
		t.Fatalf("read release manifest: %v", err)
	}
	content := string(manifest)
	for _, required := range []string{
		`<dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>`,
		`<dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2, PerMonitor</dpiAwareness>`,
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("release manifest is missing %q", required)
		}
	}
}

//go:build e2e

package main

import (
	"strings"
	"testing"
)

// An e2e build refuses to serve unless KIRA_E2E=1, before reading config.
func TestServeE2EGuard(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("KIRA_E2E", "")
	code, out, errOut := runCmd(t, "serve")
	if code == 0 || !strings.Contains(errOut, "KIRA_E2E=1") || out != "" {
		t.Fatalf("= %d %q %q", code, out, errOut)
	}
}

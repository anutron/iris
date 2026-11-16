package verbs

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A restart script that backgrounds a long-lived child still holding the
// inherited stdout/stderr must not block runArgv once the script itself exits.
func TestRunArgvDetachedChildHoldingStdoutDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "restart.sh")
	writeExecScript(t, script, "#!/bin/sh\necho relaunching\n(sleep 30 &) \nexit 0\n")
	start := time.Now()
	out, err := runArgv(context.Background(), 20*time.Second, nil, script)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if el := time.Since(start); el > 10*time.Second {
		t.Fatalf("blocked %s on detached child", el)
	}
	if !strings.Contains(out, "relaunching") {
		t.Fatalf("output = %q", out)
	}
}

// On timeout the error names the deadline and the partial output is kept, even
// when a grandchild holds the output open.
func TestRunArgvTimeoutIncludesPartialOutput(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "slow.sh")
	writeExecScript(t, script, "#!/bin/sh\necho step-one-done\nsleep 30 &\nsleep 30\n")
	start := time.Now()
	out, err := runArgv(context.Background(), 1*time.Second, nil, script)
	if err == nil || !strings.Contains(err.Error(), "timed out after 1s") {
		t.Fatalf("err = %v", err)
	}
	if el := time.Since(start); el > 10*time.Second {
		t.Fatalf("blocked %s after timeout", el)
	}
	if !strings.Contains(out, "step-one-done") {
		t.Fatalf("partial output missing: %q", out)
	}
}

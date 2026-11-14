package verbs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func isolateCheckLogs(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
}

func TestBoundCheckOutputBoundaryAndRawBytes(t *testing.T) {
	isolateCheckLogs(t)
	small := strings.Repeat("x", MaxCheckOutputBytes)
	result := &RunChecksResult{Output: small}
	if err := boundCheckOutput(result); err != nil {
		t.Fatal(err)
	}
	if result.Output != small || result.OutputTruncated || result.LogPath != "" {
		t.Fatal("output at the inclusive limit must remain unchanged")
	}

	// The tail boundary splits a multibyte rune; the final bytes are invalid
	// UTF-8. JSON must remain valid while the file keeps all original bytes.
	full := "prefix" + strings.Repeat("界", MaxCheckOutputBytes/3+1) + "\xff\xfe"
	result = &RunChecksResult{Output: full}
	if err := boundCheckOutput(result); err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(result.Output) || len(result.Output) > MaxCheckOutputBytes || !result.OutputTruncated || result.OutputBytes != len(full) {
		t.Fatal("tail is not bounded valid UTF-8")
	}
	if result.Output != strings.Repeat("界", 21844)+"?" {
		t.Fatal("tail must discard the partial leading rune and replace invalid trailing bytes")
	}
	saved, err := os.ReadFile(result.LogPath)
	if err != nil || string(saved) != full {
		t.Fatalf("raw output changed: %v", err)
	}
}

func TestBoundCheckOutputFailureExcerpt(t *testing.T) {
	isolateCheckLogs(t)
	full := "\x1b[31mFailures:\x1b[0m\r\n" + strings.Repeat("界", 30000) + "\r\nFinished in 1 second\r\n13300 examples, 2 failures, 3 pending\r\n"
	result := &RunChecksResult{Output: full}
	if err := boundCheckOutput(result); err != nil {
		t.Fatal(err)
	}
	if result.Summary == nil || *result.Summary != (CheckSummary{Examples: 13300, Failures: 2, Pending: 3}) {
		t.Fatalf("missing colorized CRLF summary: %+v", result.Summary)
	}
	if !strings.HasPrefix(result.FailureSection, "Failures:\n") || !utf8.ValidString(result.FailureSection) || len(result.FailureSection) > 16<<10 || !result.FailureSectionTruncated {
		t.Fatal("failure excerpt must be bounded, valid UTF-8 and marked truncated")
	}
}

func TestRunChecksLargeTimeoutPreservesOutput(t *testing.T) {
	isolateCheckLogs(t)
	previous := worktreeRunTimeout
	// Allow process startup under the full race suite before the ceiling
	// exercises cancellation; the script itself sleeps well past it.
	worktreeRunTimeout = 5 * time.Second
	t.Cleanup(func() { worktreeRunTimeout = previous })
	src, wt := setupRepoWithWorktree(t, "large-timeout")
	full := strings.Repeat("timeout progress\n", 10000)
	if err := os.WriteFile(filepath.Join(wt, "synthetic.log"), []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecScript(t, filepath.Join(wt, "script", "iris-check"), "#!/bin/sh\ncat synthetic.log\nsleep 30\n")
	result, err := RunChecks(context.Background(), stubArgus(t, src, wt), "task-timeout", RunChecksOptions{Check: "test"})
	var checkErr *CheckExitError
	if !errors.As(err, &checkErr) || !strings.Contains(err.Error(), "[timeout]") || result == nil || result.ExitCode != -1 || !result.OutputTruncated {
		t.Fatalf("missing bounded timeout result: %v", err)
	}
	saved, readErr := os.ReadFile(result.LogPath)
	if readErr != nil || string(saved) != full {
		t.Fatalf("timeout log lost: %v", readErr)
	}
}

func TestRunChecksTimeoutAndLogFailureReportsBoth(t *testing.T) {
	previous := worktreeRunTimeout
	worktreeRunTimeout = 5 * time.Second
	t.Cleanup(func() { worktreeRunTimeout = previous })
	src, wt := setupRepoWithWorktree(t, "timeout-log-failure")
	full := strings.Repeat("timeout progress\n", 10000)
	if err := os.WriteFile(filepath.Join(wt, "synthetic.log"), []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecScript(t, filepath.Join(wt, "script", "iris-check"), "#!/bin/sh\ncat synthetic.log\nsleep 30\n")
	t.Setenv("HOME", "/dev/null")
	t.Setenv("XDG_CACHE_HOME", "/dev/null")
	result, err := RunChecks(context.Background(), stubArgus(t, src, wt), "task-timeout", RunChecksOptions{Check: "test"})
	if err == nil || !strings.Contains(err.Error(), "[timeout]") || !strings.Contains(err.Error(), "preserve full check log") {
		t.Fatalf("both failures must remain visible: %v", err)
	}
	if result == nil || result.ExitCode != -1 || !result.OutputTruncated || result.LogPath != "" {
		t.Fatal("missing bounded timeout result without a saved log")
	}
}

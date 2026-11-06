package verbs

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// grandchildScript backgrounds a nested grandchild (script -> bash -> sleep)
// that records its PID, then blocks so the run hits the ceiling.
func grandchildScript(pidFile string) string {
	return "#!/usr/bin/env bash\n" +
		"bash -c 'sleep 60 & echo $! > '\"'" + pidFile + "'\"'; wait' &\n" +
		"echo script-started\n" +
		"wait\n"
}

// readPID waits for the grandchild's PID file and parses it.
func readPID(t *testing.T, pidFile string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("grandchild never recorded its pid in %s", pidFile)
	return 0
}

// requireGone fails unless pid is gone (ESRCH) within a few seconds.
func requireGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL) // don't leak it from the test
	t.Fatalf("grandchild %d survived the ceiling kill", pid)
}

// Worktree-runs scenario: the ceiling kills grandchildren.
// Not parallel: it shrinks the package-level ceiling.
func TestRunChecks_CeilingKillsGrandchildren(t *testing.T) {
	prev := worktreeRunTimeout
	worktreeRunTimeout = 2 * time.Second
	t.Cleanup(func() { worktreeRunTimeout = prev })

	src, wt := setupRepoWithWorktree(t, "check-pgkill")
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	writeExecScript(t, filepath.Join(wt, "script", "iris-check"), grandchildScript(pidFile))
	client := stubArgus(t, src, wt)

	_, err := RunChecks(context.Background(), client, "task-pgkill", RunChecksOptions{Check: "test"})
	if err == nil || !strings.Contains(err.Error(), "[timeout]") {
		t.Fatalf("expected [timeout] error, got %v", err)
	}
	requireGone(t, readPID(t, pidFile))
}

// Worktree-runs scenario: the ceiling kill applies to builds too.
func TestRunBuild_CeilingKillsGrandchildren(t *testing.T) {
	prev := worktreeRunTimeout
	worktreeRunTimeout = 2 * time.Second
	t.Cleanup(func() { worktreeRunTimeout = prev })

	src, wt := setupRepoWithWorktree(t, "build-pgkill")
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	writeExecScript(t, filepath.Join(wt, "script", "iris-build"), grandchildScript(pidFile))
	client := stubArgus(t, src, wt)

	_, err := RunBuild(context.Background(), client, "task-build-pgkill", RunBuildOptions{})
	if err == nil || !strings.Contains(err.Error(), "[timeout]") {
		t.Fatalf("expected [timeout] error, got %v", err)
	}
	requireGone(t, readPID(t, pidFile))
}

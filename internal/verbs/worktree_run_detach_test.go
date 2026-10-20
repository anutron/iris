package verbs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Worktree-runs scenario: caller cancellation does not kill the run.
func TestRunChecks_CallerCancellationDoesNotKillCheck(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "check-detach")
	marker := filepath.Join(t.TempDir(), "finished")
	writeExecScript(t, filepath.Join(wt, "script", "iris-check"),
		"#!/usr/bin/env bash\nsleep 1.5\ntouch '"+marker+"'\necho finished\n")
	client := stubArgus(t, src, wt)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)

	result, err := RunChecks(ctx, client, "task-detach", RunChecksOptions{Check: "test"})
	if err != nil {
		t.Fatalf("check should survive caller cancellation: %v", err)
	}
	if !strings.Contains(result.Output, "finished") {
		t.Fatalf("output missing: %q", result.Output)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("script did not run to completion: %v", err)
	}
}

func TestRunBuild_CallerCancellationDoesNotKillBuild(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "build-detach")
	writeExecScript(t, filepath.Join(wt, "script", "iris-build"),
		"#!/usr/bin/env bash\nsleep 1.5\necho built\n")
	client := stubArgus(t, src, wt)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)

	result, err := RunBuild(ctx, client, "task-build-detach", RunBuildOptions{})
	if err != nil {
		t.Fatalf("build should survive caller cancellation: %v", err)
	}
	if !strings.Contains(result.Output, "built") {
		t.Fatalf("output missing: %q", result.Output)
	}
}

// Worktree-runs scenario: an orphaned grandchild does not hold the run open.
func TestRunChecks_OrphanedGrandchildDoesNotHoldRun(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "check-orphan")
	// The background sleep inherits the output pipe and outlives the script.
	writeExecScript(t, filepath.Join(wt, "script", "iris-check"),
		"#!/usr/bin/env bash\nsleep 20 &\necho script-done\n")
	client := stubArgus(t, src, wt)

	start := time.Now()
	result, err := RunChecks(context.Background(), client, "task-orphan", RunChecksOptions{Check: "test"})
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Fatalf("run held open by grandchild for %s", elapsed)
	}
	if err != nil {
		t.Fatalf("a script that exited 0 should succeed: %v", err)
	}
	if result.ExitCode != 0 || !strings.Contains(result.Output, "script-done") {
		t.Fatalf("unexpected result: %+v", result)
	}
}

// Worktree-runs scenario: the iris-owned ceiling reports [timeout].
// Not parallel: it shrinks the package-level ceiling.
func TestRunChecks_CeilingReportsTimeout(t *testing.T) {
	prev := worktreeRunTimeout
	worktreeRunTimeout = 500 * time.Millisecond
	t.Cleanup(func() { worktreeRunTimeout = prev })

	src, wt := setupRepoWithWorktree(t, "check-ceiling")
	writeExecScript(t, filepath.Join(wt, "script", "iris-check"),
		"#!/usr/bin/env bash\necho started\nexec sleep 10\n")
	client := stubArgus(t, src, wt)

	start := time.Now()
	_, err := RunChecks(context.Background(), client, "task-ceiling", RunChecksOptions{Check: "test"})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("ceiling did not bound the run: %s", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "[timeout]") || !strings.Contains(err.Error(), "started") {
		t.Fatalf("expected [timeout] error carrying output, got %v", err)
	}
}

package verbs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cancelOnceStarted cancels the returned context as soon as the script
// creates startedMarker, so the cancellation lands while the script runs,
// never while Resolve's git calls are still in flight (which a fixed delay
// can't guarantee under -race and full-suite load).
func cancelOnceStarted(t *testing.T, startedMarker string) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(startedMarker); err == nil {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	return ctx
}

// Worktree-runs scenario: caller cancellation does not kill the run.
func TestRunChecks_CallerCancellationDoesNotKillCheck(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "check-detach")
	dir := t.TempDir()
	started, marker := filepath.Join(dir, "started"), filepath.Join(dir, "finished")
	writeExecScript(t, filepath.Join(wt, "script", "iris-check"),
		"#!/usr/bin/env bash\ntouch '"+started+"'\nsleep 1\ntouch '"+marker+"'\necho finished\n")
	client := stubArgus(t, src, wt)

	ctx := cancelOnceStarted(t, started)

	result, err := RunChecks(ctx, client, "task-detach", RunChecksOptions{Check: "test"})
	if err != nil {
		t.Fatalf("check should survive caller cancellation: %v", err)
	}
	if ctx.Err() == nil {
		t.Fatal("test bug: the caller context was never cancelled")
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
	started := filepath.Join(t.TempDir(), "started")
	writeExecScript(t, filepath.Join(wt, "script", "iris-build"),
		"#!/usr/bin/env bash\ntouch '"+started+"'\nsleep 1\necho built\n")
	client := stubArgus(t, src, wt)

	ctx := cancelOnceStarted(t, started)

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

// Worktree-runs scenario: the ceiling also bounds waiting for the worktree
// lock. Not parallel: it shrinks the package-level ceiling.
func TestRunChecks_CeilingBoundsLockWait(t *testing.T) {
	prev := worktreeRunTimeout
	worktreeRunTimeout = 300 * time.Millisecond
	t.Cleanup(func() { worktreeRunTimeout = prev })

	src, wt := setupRepoWithWorktree(t, "check-lockwait")
	ran := filepath.Join(t.TempDir(), "ran")
	writeExecScript(t, filepath.Join(wt, "script", "iris-check"),
		"#!/usr/bin/env bash\ntouch '"+ran+"'\n")
	client := stubArgus(t, src, wt)
	resolved, err := Resolve(context.Background(), client, "task-lockwait")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Another run holds the worktree lock for the whole test.
	mu, err := lockWorktreeCtx(context.Background(), resolved.WorktreePath)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	defer mu.Unlock()

	_, err = RunChecks(context.Background(), client, "task-lockwait", RunChecksOptions{Check: "test"})
	if err == nil || !strings.Contains(err.Error(), "[timeout]") || !strings.Contains(err.Error(), "worktree lock") {
		t.Fatalf("expected [timeout] waiting for the worktree lock, got %v", err)
	}
	if _, statErr := os.Stat(ran); statErr == nil {
		t.Fatal("script must not run without the lock")
	}
}

// The start hook fires only once the run holds the worktree lock.
func TestRunChecks_StartHookFiresAfterLock(t *testing.T) {
	t.Parallel()
	src, wt := setupRepoWithWorktree(t, "check-starthook")
	writeExecScript(t, filepath.Join(wt, "script", "iris-check"), "#!/usr/bin/env bash\necho ok\n")
	client := stubArgus(t, src, wt)
	resolved, err := Resolve(context.Background(), client, "task-hook")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	mu, err := lockWorktreeCtx(context.Background(), resolved.WorktreePath)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}

	started := make(chan struct{})
	ctx := WithStartHook(context.Background(), func() { close(started) })
	done := make(chan error, 1)
	go func() {
		_, err := RunChecks(ctx, client, "task-hook", RunChecksOptions{Check: "test"})
		done <- err
	}()

	select {
	case <-started:
		t.Fatal("start hook fired while another run held the lock")
	case <-time.After(500 * time.Millisecond):
	}
	mu.Unlock()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("start hook never fired after the lock was released")
	}
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
}

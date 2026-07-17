package verbs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anutron/iris/internal/argus"
)

// remoteRef returns `git rev-parse <ref>` on bare. Used to assert that
// negative-path tests left origin's refs untouched.
func remoteRef(t *testing.T, bare, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", bare, "rev-parse", ref).CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// setupRepoWithBareAndWorktree extends setupRepoWithWorktree by returning
// the bare origin path so tests can inspect/poke origin directly.
func setupRepoWithBareAndWorktree(t *testing.T, slug string) (sourceRepo, worktreePath, bare string) {
	t.Helper()
	tmp := t.TempDir()
	bare = filepath.Join(tmp, "origin.git")
	src := filepath.Join(tmp, "src")
	wt := filepath.Join(tmp, "wt")
	g := gitRunner(t)

	g("", "init", "--bare", "-b", "main", bare)
	g("", "clone", bare, src)
	g(src, "config", "user.email", "iris-test@example.com")
	g(src, "config", "user.name", "iris-test")
	g(src, "commit", "--allow-empty", "-m", "initial")
	g(src, "push", "-u", "origin", "main")
	g(src, "remote", "set-head", "origin", "main")
	branch := "argus/" + slug
	g(src, "branch", branch)
	g(src, "worktree", "add", wt, branch)
	g(wt, "config", "user.email", "iris-test@example.com")
	g(wt, "config", "user.name", "iris-test")
	g(wt, "commit", "--allow-empty", "-m", "work on "+branch)
	return src, wt, bare
}

func TestPush_HappyPath(t *testing.T) {
	t.Parallel()
	src, wt, bare := setupRepoWithBareAndWorktree(t, "push-happy")
	client := stubArgus(t, src, wt)

	result, err := Push(context.Background(), client, "task-push", PushOptions{})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if !result.Pushed {
		t.Fatal("expected Pushed=true")
	}
	if result.Branch != "argus/push-happy" {
		t.Fatalf("unexpected branch: %q", result.Branch)
	}
	localSHA := headSHA(t, wt)
	if result.RemoteSHA != localSHA {
		t.Fatalf("remote sha mismatch: got %q, want %q", result.RemoteSHA, localSHA)
	}
	remote := remoteRef(t, bare, "argus/push-happy")
	if remote != localSHA {
		t.Fatalf("bare origin ref mismatch: got %q, want %q", remote, localSHA)
	}
}

func TestPush_RefusesDefaultBranch(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "origin.git")
	src := filepath.Join(tmp, "src")
	wt := filepath.Join(tmp, "wt")
	g := gitRunner(t)

	g("", "init", "--bare", "-b", "main", bare)
	g("", "clone", bare, src)
	g(src, "config", "user.email", "x@y.z")
	g(src, "config", "user.name", "x")
	g(src, "commit", "--allow-empty", "-m", "initial")
	g(src, "push", "-u", "origin", "main")
	g(src, "remote", "set-head", "origin", "main")
	// Move src off main so we can worktree main.
	g(src, "switch", "-c", "iris-test-host")
	g(src, "worktree", "add", wt, "main")

	client := stubArgus(t, src, wt)
	beforeRemote := remoteRef(t, bare, "main")
	_, err := Push(context.Background(), client, "task-default", PushOptions{})
	if err == nil {
		t.Fatal("expected error refusing default branch, got nil")
	}
	if !strings.Contains(err.Error(), "default branch") {
		t.Fatalf("unexpected error: %v", err)
	}
	if afterRemote := remoteRef(t, bare, "main"); afterRemote != beforeRemote {
		t.Fatalf("expected origin/main unchanged: before=%s after=%s", beforeRemote, afterRemote)
	}
}

func TestPush_RefusesUnknownTaskID(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t)
	_, err := Push(context.Background(), client, "ghost-task", PushOptions{})
	if err == nil {
		t.Fatal("expected error for unknown task, got nil")
	}
	if !strings.Contains(err.Error(), "ghost-task") {
		t.Fatalf("expected error to name task id, got: %v", err)
	}
}

func TestPush_ForceWithLeaseSucceeds(t *testing.T) {
	t.Parallel()
	src, wt, bare := setupRepoWithBareAndWorktree(t, "push-lease")
	client := stubArgus(t, src, wt)

	// Initial push so the remote tracks the branch.
	if _, err := Push(context.Background(), client, "task-lease", PushOptions{}); err != nil {
		t.Fatalf("initial push: %v", err)
	}

	// Amend on the worktree to rewrite history; --force-with-lease should
	// succeed because the upstream is exactly what we expect.
	g := gitRunner(t)
	g(wt, "commit", "--allow-empty", "--amend", "-m", "amended")

	result, err := Push(context.Background(), client, "task-lease", PushOptions{ForceWithLease: true})
	if err != nil {
		t.Fatalf("force-with-lease push: %v", err)
	}
	if !result.Pushed {
		t.Fatal("expected Pushed=true")
	}
	if result.RemoteSHA != headSHA(t, wt) {
		t.Fatalf("remote SHA after amend mismatch: got %q, want %q", result.RemoteSHA, headSHA(t, wt))
	}
	if remoteRef(t, bare, "argus/push-lease") != result.RemoteSHA {
		t.Fatal("bare origin did not receive the amended ref")
	}
}

func TestPush_NonFastForwardErrorsWithoutForce(t *testing.T) {
	t.Parallel()
	src, wt, bare := setupRepoWithBareAndWorktree(t, "push-nonff")
	client := stubArgus(t, src, wt)

	// First push so origin tracks the branch.
	if _, err := Push(context.Background(), client, "task-nonff", PushOptions{}); err != nil {
		t.Fatalf("initial push: %v", err)
	}

	// Race a competing commit onto origin via a sidecar clone. After this,
	// the worktree's branch is behind origin.
	tmp := t.TempDir()
	side := filepath.Join(tmp, "side")
	g := gitRunner(t)
	g("", "clone", bare, side)
	g(side, "config", "user.email", "x@y.z")
	g(side, "config", "user.name", "x")
	g(side, "checkout", "argus/push-nonff")
	if err := os.WriteFile(filepath.Join(side, "side.txt"), []byte("competing\n"), 0o644); err != nil {
		t.Fatalf("write side: %v", err)
	}
	g(side, "add", "side.txt")
	g(side, "commit", "-m", "competing commit")
	g(side, "push", "origin", "argus/push-nonff")

	// Make a divergent commit on the worktree.
	g(wt, "commit", "--allow-empty", "-m", "wt divergent")

	_, err := Push(context.Background(), client, "task-nonff", PushOptions{})
	if err == nil {
		t.Fatal("expected non-fast-forward error, got nil")
	}
	if !strings.Contains(err.Error(), "push") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Sanity: origin should still hold the competing commit, not the worktree's divergent one.
	remote := remoteRef(t, bare, "argus/push-nonff")
	if remote == headSHA(t, wt) {
		t.Fatal("expected origin ref to remain at the competing commit after the rejected push")
	}
	_ = src // keep src in scope to silence vet (its presence makes the setup readable)
}

// TestPush_BranchOverride verifies that when opts.Branch is non-empty the verb
// pushes the named branch (not the task's resolved branch) and reports it.
func TestPush_BranchOverride(t *testing.T) {
	t.Parallel()
	src, wt, bare := setupRepoWithBareAndWorktree(t, "push-override")
	client := stubArgus(t, src, wt)

	// Create a second branch in the worktree's source repo so there is something
	// to push.  The worktree's own branch is "argus/push-override"; we create
	// "feature-x" from the same HEAD. The push succeeds because it's a brand-new
	// ref on origin, not because it's ahead of an existing one.
	g := gitRunner(t)
	g(src, "branch", "feature-x")

	result, err := Push(context.Background(), client, "task-override", PushOptions{Branch: "feature-x"})
	if err != nil {
		t.Fatalf("push with branch override: %v", err)
	}
	if !result.Pushed {
		t.Fatal("expected Pushed=true")
	}
	if result.Branch != "feature-x" {
		t.Fatalf("result.Branch: got %q, want %q", result.Branch, "feature-x")
	}
	// origin must have the override branch, not the task branch.
	if remoteRef(t, bare, "feature-x") == "" {
		t.Fatal("expected feature-x to be pushed to bare origin")
	}
	if remoteRef(t, bare, "argus/push-override") != "" {
		t.Fatal("expected argus/push-override NOT to be pushed to bare origin")
	}
}

// TestPush_BranchOverridePushesTaskWorkNotStaleLocalRef verifies that a
// branch= override publishes the task's actual HEAD, not whatever
// unrelated local ref already happens to be named after the override in
// the shared source repo. Regression test for a bug where `git push
// <remote> <effective>` read the literal local ref named <effective> —
// silently pushing stale history (and reporting success) instead of the
// task's real work whenever a same-named stale branch already existed.
func TestPush_BranchOverridePushesTaskWorkNotStaleLocalRef(t *testing.T) {
	t.Parallel()
	src, wt, bare := setupRepoWithBareAndWorktree(t, "push-stale-override")
	client := stubArgus(t, src, wt)

	// A pre-existing local branch in src, same name as the override target,
	// but branched from src's own current HEAD — i.e. the *original* commit,
	// not the worktree's later work commit. This mirrors the stale-ref shape
	// seen in production: a shared source repo accumulates same-named
	// branches from unrelated earlier tasks.
	g := gitRunner(t)
	g(src, "branch", "stale-target")

	result, err := Push(context.Background(), client, "task-stale-override", PushOptions{Branch: "stale-target"})
	if err != nil {
		t.Fatalf("push with branch override: %v", err)
	}
	if !result.Pushed {
		t.Fatal("expected Pushed=true")
	}

	wantSHA := headSHA(t, wt)
	if result.RemoteSHA != wantSHA {
		t.Fatalf("result.RemoteSHA: got %q, want worktree HEAD %q (task work must be pushed, not the stale local ref)", result.RemoteSHA, wantSHA)
	}
	if got := remoteRef(t, bare, "stale-target"); got != wantSHA {
		t.Fatalf("bare origin stale-target: got %q, want worktree HEAD %q", got, wantSHA)
	}
}

// TestPush_BranchOverrideDefaultBranchRefused verifies that the default-branch
// refusal applies to the override, not just the resolved task branch.
func TestPush_BranchOverrideDefaultBranchRefused(t *testing.T) {
	t.Parallel()
	src, wt, bare := setupRepoWithBareAndWorktree(t, "push-override-default")
	client := stubArgus(t, src, wt)
	beforeRemote := remoteRef(t, bare, "main")

	_, err := Push(context.Background(), client, "task-odef", PushOptions{Branch: "main"})
	if err == nil {
		t.Fatal("expected error refusing default branch via override, got nil")
	}
	if !strings.Contains(err.Error(), "default branch") {
		t.Fatalf("unexpected error: %v", err)
	}
	if afterRemote := remoteRef(t, bare, "main"); afterRemote != beforeRemote {
		t.Fatalf("expected origin/main unchanged: before=%s after=%s", beforeRemote, afterRemote)
	}
}

// TestPush_BranchOverrideRejectsLeadingDash verifies that a caller-supplied
// branch override beginning with '-' is rejected before git runs, so it cannot
// smuggle flags into git push.
func TestPush_BranchOverrideRejectsLeadingDash(t *testing.T) {
	t.Parallel()
	src, wt, bare := setupRepoWithBareAndWorktree(t, "push-override-dash")
	client := stubArgus(t, src, wt)
	beforeRemote := remoteRef(t, bare, "argus/push-override-dash")

	_, err := Push(context.Background(), client, "task-dash", PushOptions{Branch: "--upload-pack=evil"})
	if err == nil {
		t.Fatal("expected error rejecting leading-dash branch override, got nil")
	}
	if !strings.Contains(err.Error(), "must not begin with '-'") {
		t.Fatalf("unexpected error: %v", err)
	}
	if afterRemote := remoteRef(t, bare, "argus/push-override-dash"); afterRemote != beforeRemote {
		t.Fatalf("expected origin unchanged: before=%s after=%s", beforeRemote, afterRemote)
	}
}

// TestPush_NoBranchOverridePreservesTaskBranch verifies backward compatibility:
// omitting opts.Branch pushes the task's resolved branch as before.
func TestPush_NoBranchOverridePreservesTaskBranch(t *testing.T) {
	t.Parallel()
	src, wt, bare := setupRepoWithBareAndWorktree(t, "push-nooverride")
	client := stubArgus(t, src, wt)

	result, err := Push(context.Background(), client, "task-nooverride", PushOptions{})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if result.Branch != "argus/push-nooverride" {
		t.Fatalf("result.Branch: got %q, want %q", result.Branch, "argus/push-nooverride")
	}
	if remoteRef(t, bare, "argus/push-nooverride") == "" {
		t.Fatal("expected argus/push-nooverride to be pushed to bare origin")
	}
}

// TestPush_RemoteOverride verifies that when opts.Remote is non-empty the verb
// pushes to the named remote (not origin) and reports it.
func TestPush_RemoteOverride(t *testing.T) {
	t.Parallel()
	src, wt, bare := setupRepoWithBareAndWorktree(t, "push-remote")
	client := stubArgus(t, src, wt)

	// Add a second bare remote "upstream" to the source repo.
	tmp := t.TempDir()
	upstreamBare := filepath.Join(tmp, "upstream.git")
	g := gitRunner(t)
	g("", "init", "--bare", "-b", "main", upstreamBare)
	g(src, "remote", "add", "upstream", upstreamBare)

	result, err := Push(context.Background(), client, "task-remote", PushOptions{Remote: "upstream"})
	if err != nil {
		t.Fatalf("push with remote override: %v", err)
	}
	if !result.Pushed {
		t.Fatal("expected Pushed=true")
	}
	if result.Remote != "upstream" {
		t.Fatalf("result.Remote: got %q, want %q", result.Remote, "upstream")
	}
	localSHA := headSHA(t, wt)
	if result.RemoteSHA != localSHA {
		t.Fatalf("remote sha mismatch: got %q want %q", result.RemoteSHA, localSHA)
	}
	// The branch must land on the upstream bare, NOT on origin.
	if remoteRef(t, upstreamBare, "argus/push-remote") != localSHA {
		t.Fatal("expected argus/push-remote on the upstream bare")
	}
	if remoteRef(t, bare, "argus/push-remote") != "" {
		t.Fatal("expected origin bare to NOT receive the branch")
	}
}

// TestPush_RefusesUnknownRemote verifies a remote that is not configured in the
// source repo is refused before any push.
func TestPush_RefusesUnknownRemote(t *testing.T) {
	t.Parallel()
	src, wt, bare := setupRepoWithBareAndWorktree(t, "push-badremote")
	client := stubArgus(t, src, wt)
	before := remoteRef(t, bare, "argus/push-badremote")

	_, err := Push(context.Background(), client, "task-badremote", PushOptions{Remote: "nope"})
	if err == nil {
		t.Fatal("expected error for unknown remote, got nil")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected error to name the unknown remote, got: %v", err)
	}
	if after := remoteRef(t, bare, "argus/push-badremote"); after != before {
		t.Fatalf("expected origin unchanged: before=%s after=%s", before, after)
	}
}

// TestPush_RemoteRejectsLeadingDash verifies a remote override beginning with
// '-' is rejected before git runs.
func TestPush_RemoteRejectsLeadingDash(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "push-remote-dash")
	client := stubArgus(t, src, wt)

	_, err := Push(context.Background(), client, "task-remote-dash", PushOptions{Remote: "--upload-pack=evil"})
	if err == nil {
		t.Fatal("expected error rejecting leading-dash remote, got nil")
	}
	if !strings.Contains(err.Error(), "must not begin with '-'") {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = src
}

// TestPush_NoRemoteDefaultsToOrigin verifies backward compatibility: omitting
// opts.Remote pushes to origin and reports remote="origin".
func TestPush_NoRemoteDefaultsToOrigin(t *testing.T) {
	t.Parallel()
	src, wt, bare := setupRepoWithBareAndWorktree(t, "push-noremote")
	client := stubArgus(t, src, wt)

	result, err := Push(context.Background(), client, "task-noremote", PushOptions{})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if result.Remote != "origin" {
		t.Fatalf("result.Remote: got %q, want %q", result.Remote, "origin")
	}
	if remoteRef(t, bare, "argus/push-noremote") == "" {
		t.Fatal("expected argus/push-noremote pushed to origin bare")
	}
}

// Concurrency invariant: Push shares repoLocks with MergeToMaster. We don't
// need a second test for this — the merge_to_master serialize test already
// validates the lock map; this is a coverage check that Push compiles
// against it.
// TestPush_CallerContextCancellationDoesNotKillInFlightPush is the
// end-to-end regression test for the context-decoupling fix: cancelling
// the ctx passed into Push must not kill an in-flight `git push`, because
// the transfer runs under iris's own detached timeout.
func TestPush_CallerContextCancellationDoesNotKillInFlightPush(t *testing.T) {
	t.Parallel()
	src, wt, bare := setupRepoWithBareAndWorktree(t, "push-decouple")
	client := stubArgus(t, src, wt)

	marker := filepath.Join(t.TempDir(), "prereceive-started")
	setSlowPreReceiveHook(t, bare, marker, 300*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	type outcome struct {
		result *PushResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := Push(ctx, client, "task-push-decouple", PushOptions{})
		done <- outcome{result, err}
	}()

	waitForMarker(t, marker, 2*time.Second)
	cancel() // simulate argus's client giving up and tearing down r.Context()

	select {
	case o := <-done:
		if o.err != nil {
			t.Fatalf("Push returned error after caller ctx cancellation: %v", o.err)
		}
		if !o.result.Pushed {
			t.Fatal("expected Pushed=true")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Push did not return in time")
	}

	if remoteRef(t, bare, "argus/push-decouple") == "" {
		t.Fatal("expected argus/push-decouple to have been pushed to bare origin despite caller ctx cancellation")
	}
}

// TestPush_ConfiguredGitTransferTimeoutClassifiesAsTimeout proves Push
// wires the source repo's .iris.toml git_transfer_timeout_seconds into the
// actual git push invocation, and that a too-short configured timeout
// surfaces as a classified *GitTransferError, not an opaque failure.
func TestPush_ConfiguredGitTransferTimeoutClassifiesAsTimeout(t *testing.T) {
	t.Parallel()
	src, wt, bare := setupRepoWithBareAndWorktree(t, "push-timeout")
	client := stubArgus(t, src, wt)

	marker := filepath.Join(t.TempDir(), "prereceive-started")
	setSlowPreReceiveHook(t, bare, marker, 3*time.Second)

	toml := "schema_version = 1\ngit_transfer_timeout_seconds = 1\n"
	if err := os.WriteFile(filepath.Join(src, ".iris.toml"), []byte(toml), 0o644); err != nil {
		t.Fatalf("write .iris.toml: %v", err)
	}

	start := time.Now()
	_, err := Push(context.Background(), client, "task-push-timeout", PushOptions{})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if !IsGitTransferTimeout(err) {
		t.Fatalf("expected IsGitTransferTimeout(err) = true, got err: %v", err)
	}
	if elapsed > 2500*time.Millisecond {
		t.Fatalf("Push took %s; expected it to respect the configured 1s timeout, not the hook's 3s sleep", elapsed)
	}
}

func TestPush_AllowlistRejection(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "push-denied")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/tasks/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "x", "worktree_path": wt})
		case r.URL.Path == "/api/projects/full":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"name": "other", "path": "/some/other/repo"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client := argus.New(srv.URL, "stub-token")

	_, err := Push(context.Background(), client, "task-denied", PushOptions{})
	if err == nil {
		t.Fatal("expected allowlist refusal, got nil")
	}
	if !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("expected allowlist error, got: %v", err)
	}
	_ = src
}

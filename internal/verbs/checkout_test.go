package verbs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCheckout_HappyPath(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "co-happy")
	g := gitRunner(t)
	g(src, "branch", "feature/x", "origin/main")
	client := stubArgus(t, src, wt)

	priorBranch := currentBranchOrFail(t, src)
	priorHead := headSHA(t, src)

	result, err := Checkout(context.Background(), CheckoutInput{
		Client: client, TaskID: "t", Branch: "feature/x",
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if !result.CheckedOut {
		t.Fatal("expected CheckedOut=true")
	}
	if result.Branch != "feature/x" {
		t.Fatalf("unexpected branch: %q", result.Branch)
	}
	if result.PriorBranch != priorBranch {
		t.Fatalf("PriorBranch mismatch: %q vs %q", result.PriorBranch, priorBranch)
	}
	if result.PriorHead != priorHead {
		t.Fatalf("PriorHead mismatch: %q vs %q", result.PriorHead, priorHead)
	}
	if got := currentBranchOrFail(t, src); got != "feature/x" {
		t.Fatalf("source repo not on feature/x: on %s", got)
	}
}

func TestCheckout_RefusesEmptyBranch(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "co-empty")
	client := stubArgus(t, src, wt)
	_, err := Checkout(context.Background(), CheckoutInput{
		Client: client, TaskID: "t", Branch: "",
	})
	if err == nil || !strings.Contains(err.Error(), "branch") {
		t.Fatalf("expected error mentioning branch field, got: %v", err)
	}
}

func TestCheckout_RefusesLeadingDashBranch(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "co-dash")
	client := stubArgus(t, src, wt)
	_, err := Checkout(context.Background(), CheckoutInput{
		Client: client, TaskID: "t", Branch: "--upload-pack=evil",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid branch name") {
		t.Fatalf("expected 'invalid branch name' error, got: %v", err)
	}
}

func TestCheckout_RefusesUnknownBranch(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "co-unknown")
	client := stubArgus(t, src, wt)
	priorBranch := currentBranchOrFail(t, src)
	_, err := Checkout(context.Background(), CheckoutInput{
		Client: client, TaskID: "t", Branch: "no-such-branch",
	})
	if err == nil {
		t.Fatal("expected error for unknown branch")
	}
	if currentBranchOrFail(t, src) != priorBranch {
		t.Fatal("source repo branch changed on failed checkout")
	}
}

func TestCheckout_PropagatesDirtyTreeRefusal(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "co-dirty")
	g := gitRunner(t)
	g(src, "branch", "other", "origin/main")
	// Make a tracked-file conflict: modify an existing file (the initial
	// commit added none, so commit one first).
	if err := os.WriteFile(filepath.Join(src, "tracked.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("write tracked.txt: %v", err)
	}
	g(src, "add", "tracked.txt")
	g(src, "commit", "-m", "track file")
	g(src, "push", "origin", "main")
	g(src, "branch", "-f", "other", "main")
	// Modify on `main`; switching to `other` (which has different content)
	// would lose changes — git refuses.
	g(src, "checkout", "other")
	if err := os.WriteFile(filepath.Join(src, "tracked.txt"), []byte("v2-on-other\n"), 0o644); err != nil {
		t.Fatalf("write on other: %v", err)
	}
	g(src, "add", "tracked.txt")
	g(src, "commit", "-m", "other branch divergence")
	g(src, "checkout", "main")
	// Now make a dirty modification on main.
	if err := os.WriteFile(filepath.Join(src, "tracked.txt"), []byte("dirty-main\n"), 0o644); err != nil {
		t.Fatalf("write dirty: %v", err)
	}

	client := stubArgus(t, src, wt)
	priorBranch := currentBranchOrFail(t, src)
	_, err := Checkout(context.Background(), CheckoutInput{
		Client: client, TaskID: "t", Branch: "other", Force: false,
	})
	if err == nil {
		t.Fatal("expected error when switching with dirty tree")
	}
	if currentBranchOrFail(t, src) != priorBranch {
		t.Fatal("source repo switched branch despite refusing dirty tree")
	}
}

func TestCheckout_ForceDiscardsUncommittedChanges(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "co-force-dirty")
	g := gitRunner(t)
	if err := os.WriteFile(filepath.Join(src, "tracked.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("write tracked.txt: %v", err)
	}
	g(src, "add", "tracked.txt")
	g(src, "commit", "-m", "track file")
	g(src, "push", "origin", "main")
	g(src, "branch", "other", "main")
	g(src, "checkout", "other")
	if err := os.WriteFile(filepath.Join(src, "tracked.txt"), []byte("v2-on-other\n"), 0o644); err != nil {
		t.Fatalf("write on other: %v", err)
	}
	g(src, "add", "tracked.txt")
	g(src, "commit", "-m", "other branch divergence")
	g(src, "checkout", "main")
	if err := os.WriteFile(filepath.Join(src, "tracked.txt"), []byte("dirty-main\n"), 0o644); err != nil {
		t.Fatalf("write dirty: %v", err)
	}

	client := stubArgus(t, src, wt)
	result, err := Checkout(context.Background(), CheckoutInput{
		Client: client, TaskID: "t", Branch: "other", Force: true,
	})
	if err != nil {
		t.Fatalf("force checkout: %v", err)
	}
	if !result.CheckedOut {
		t.Fatal("expected CheckedOut=true")
	}
	if currentBranchOrFail(t, src) != "other" {
		t.Fatalf("source repo not on other after force checkout; on %s", currentBranchOrFail(t, src))
	}
	got, err := os.ReadFile(filepath.Join(src, "tracked.txt"))
	if err != nil {
		t.Fatalf("read tracked.txt: %v", err)
	}
	if strings.TrimSpace(string(got)) != "v2-on-other" {
		t.Fatalf("expected file from other branch; got: %q", string(got))
	}
}

func TestCheckout_ForceRecoversFromInProgressMerge(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "co-force-merge")
	g := gitRunner(t)
	// Build two divergent branches that conflict on the same file.
	if err := os.WriteFile(filepath.Join(src, "conflict.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write conflict.txt: %v", err)
	}
	g(src, "add", "conflict.txt")
	g(src, "commit", "-m", "base")
	g(src, "branch", "side", "main")
	g(src, "checkout", "side")
	if err := os.WriteFile(filepath.Join(src, "conflict.txt"), []byte("side-version\n"), 0o644); err != nil {
		t.Fatalf("write side: %v", err)
	}
	g(src, "add", "conflict.txt")
	g(src, "commit", "-m", "side commit")
	g(src, "checkout", "main")
	if err := os.WriteFile(filepath.Join(src, "conflict.txt"), []byte("main-version\n"), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}
	g(src, "add", "conflict.txt")
	g(src, "commit", "-m", "main commit")
	g(src, "branch", "destination", "main")

	// Trigger a conflicting merge but DO NOT abort — that's the broken state
	// iris:checkout --force is meant to recover from.
	mergeCmd := []string{"-C", src, "merge", "--no-ff", "side"}
	_ = runShellExpectFail(t, "git", mergeCmd...)
	if _, err := os.Stat(filepath.Join(src, ".git", "MERGE_HEAD")); err != nil {
		t.Fatalf("MERGE_HEAD should exist after conflicting merge: %v", err)
	}

	client := stubArgus(t, src, wt)
	result, err := Checkout(context.Background(), CheckoutInput{
		Client: client, TaskID: "t", Branch: "destination", Force: true,
	})
	if err != nil {
		t.Fatalf("force recovery: %v", err)
	}
	if !result.CheckedOut {
		t.Fatal("expected CheckedOut=true")
	}
	if currentBranchOrFail(t, src) != "destination" {
		t.Fatalf("source repo not on destination after recovery; on %s", currentBranchOrFail(t, src))
	}
	if _, err := os.Stat(filepath.Join(src, ".git", "MERGE_HEAD")); err == nil {
		t.Fatal("MERGE_HEAD still present after force recovery")
	}
}

func TestCheckout_ForceRecoversFromInProgressCherryPick(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "co-force-cp")
	g := gitRunner(t)
	// Two branches that conflict on the same file.
	if err := os.WriteFile(filepath.Join(src, "conflict.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write conflict.txt: %v", err)
	}
	g(src, "add", "conflict.txt")
	g(src, "commit", "-m", "base")
	g(src, "branch", "side", "main")
	g(src, "checkout", "side")
	if err := os.WriteFile(filepath.Join(src, "conflict.txt"), []byte("side-version\n"), 0o644); err != nil {
		t.Fatalf("write side: %v", err)
	}
	g(src, "add", "conflict.txt")
	g(src, "commit", "-m", "side commit")
	sideCommit := strings.TrimSpace(g(src, "rev-parse", "HEAD"))
	g(src, "checkout", "main")
	if err := os.WriteFile(filepath.Join(src, "conflict.txt"), []byte("main-version\n"), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}
	g(src, "add", "conflict.txt")
	g(src, "commit", "-m", "main commit")
	g(src, "branch", "destination", "main")
	_ = runShellExpectFail(t, "git", "-C", src, "cherry-pick", sideCommit)
	if _, err := os.Stat(filepath.Join(src, ".git", "CHERRY_PICK_HEAD")); err != nil {
		t.Fatalf("CHERRY_PICK_HEAD should exist after conflicting cherry-pick: %v", err)
	}

	client := stubArgus(t, src, wt)
	result, err := Checkout(context.Background(), CheckoutInput{
		Client: client, TaskID: "t", Branch: "destination", Force: true,
	})
	if err != nil {
		t.Fatalf("force recovery: %v", err)
	}
	if !result.CheckedOut {
		t.Fatal("expected CheckedOut=true")
	}
	if _, err := os.Stat(filepath.Join(src, ".git", "CHERRY_PICK_HEAD")); err == nil {
		t.Fatal("CHERRY_PICK_HEAD still present after force recovery")
	}
}

func TestCheckout_RefusesInProgressOpWhenForceFalse(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "co-refuses-inprogress")
	g := gitRunner(t)
	if err := os.WriteFile(filepath.Join(src, "conflict.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write conflict.txt: %v", err)
	}
	g(src, "add", "conflict.txt")
	g(src, "commit", "-m", "base")
	g(src, "branch", "side", "main")
	g(src, "checkout", "side")
	if err := os.WriteFile(filepath.Join(src, "conflict.txt"), []byte("side-version\n"), 0o644); err != nil {
		t.Fatalf("write side: %v", err)
	}
	g(src, "add", "conflict.txt")
	g(src, "commit", "-m", "side commit")
	g(src, "checkout", "main")
	if err := os.WriteFile(filepath.Join(src, "conflict.txt"), []byte("main-version\n"), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}
	g(src, "add", "conflict.txt")
	g(src, "commit", "-m", "main commit")
	g(src, "branch", "destination", "main")
	_ = runShellExpectFail(t, "git", "-C", src, "merge", "--no-ff", "side")

	client := stubArgus(t, src, wt)
	_, err := Checkout(context.Background(), CheckoutInput{
		Client: client, TaskID: "t", Branch: "destination", Force: false,
	})
	if err == nil {
		t.Fatal("expected refusal when force=false and a merge is in progress")
	}
	if _, statErr := os.Stat(filepath.Join(src, ".git", "MERGE_HEAD")); statErr != nil {
		t.Fatal("MERGE_HEAD should still exist when force=false refused; verb must not have aborted the merge")
	}
}

func TestCheckout_RefusesUnknownTask(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t)
	_, err := Checkout(context.Background(), CheckoutInput{
		Client: client, TaskID: "ghost", Branch: "feature/x",
	})
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected error naming task id, got: %v", err)
	}
}

func TestCheckout_LockSerializesConcurrentCalls(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "co-lock")
	g := gitRunner(t)
	g(src, "branch", "feature/x", "origin/main")
	client := stubArgus(t, src, wt)

	canon, _ := filepath.EvalSymlinks(src)
	mu := lockSourceRepo(canon)
	released := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := Checkout(context.Background(), CheckoutInput{
			Client: client, TaskID: "t", Branch: "feature/x",
		})
		if err != nil {
			t.Errorf("checkout under lock: %v", err)
		}
		close(released)
	}()
	select {
	case <-released:
		mu.Unlock()
		t.Fatal("checkout did not block on held lock")
	case <-time.After(150 * time.Millisecond):
	}
	mu.Unlock()
	wg.Wait()
}

// runShellExpectFail runs a command that is EXPECTED to exit non-zero
// (e.g., a conflicting merge or cherry-pick). It returns combined output
// for diagnostics. Failure to exit non-zero is a test setup error.
func runShellExpectFail(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err == nil {
		t.Fatalf("expected %s %v to fail; succeeded with output:\n%s", name, args, out)
	}
	return string(out)
}

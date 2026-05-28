package verbs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// setupCherryPickRepo builds on setupRepoWithBareAndWorktree by:
//   - writing a real file commit on the argus/<slug> worktree branch
//     (so there's a non-empty commit to cherry-pick),
//   - creating a `hotfix/<slug>` branch in the source repo pointing at
//     `origin/main` (the target the cherry-pick lands on).
//
// Returns (src, wt, bare, workCommit, hotfixBranch).
func setupCherryPickRepo(t *testing.T, slug string) (src, wt, bare, workCommit, hotfix string) {
	t.Helper()
	src, wt, bare = setupRepoWithBareAndWorktree(t, slug)
	g := gitRunner(t)

	// Replace the empty commit on the worktree with one that adds a file
	// so the cherry-pick is meaningful.
	path := filepath.Join(wt, "feature.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write feature.txt: %v", err)
	}
	g(wt, "add", "feature.txt")
	g(wt, "commit", "-m", "add feature.txt")
	workCommit = strings.TrimSpace(g(wt, "rev-parse", "HEAD"))

	hotfix = "hotfix/" + slug
	g(src, "branch", hotfix, "origin/main")
	return
}

func TestCherryPick_HappyPath(t *testing.T) {
	src, wt, _, commit, hotfix := setupCherryPickRepo(t, "cp-happy")
	client := stubArgus(t, src, wt)

	beforeHotfix := revParse(t, src, "refs/heads/"+hotfix)

	result, err := CherryPick(context.Background(), CherryPickInput{
		Client: client, TaskID: "t", Commit: commit, TargetBranch: hotfix,
	})
	if err != nil {
		t.Fatalf("cherry_pick: %v", err)
	}
	if !result.CherryPicked {
		t.Fatal("expected CherryPicked=true")
	}
	if result.Commit != commit {
		t.Fatalf("Commit mismatch: %q vs %q", result.Commit, commit)
	}
	if result.TargetBranch != hotfix {
		t.Fatalf("TargetBranch mismatch: %q vs %q", result.TargetBranch, hotfix)
	}
	if result.NewSHA == "" || result.NewSHA == beforeHotfix {
		t.Fatalf("NewSHA should be a fresh commit on %s; was %q (before=%q)", hotfix, result.NewSHA, beforeHotfix)
	}
	// Source repo should now be on the target branch.
	if branch := currentBranchOrFail(t, src); branch != hotfix {
		t.Fatalf("source repo should be on %s after cherry-pick; on %s", hotfix, branch)
	}
	// HEAD of hotfix should match NewSHA.
	if got := revParse(t, src, "refs/heads/"+hotfix); got != result.NewSHA {
		t.Fatalf("refs/heads/%s mismatch: got %q want %q", hotfix, got, result.NewSHA)
	}
	// File should be present in the working tree.
	if _, err := os.Stat(filepath.Join(src, "feature.txt")); err != nil {
		t.Fatalf("cherry-picked file missing in source repo: %v", err)
	}
}

func TestCherryPick_RefusesEmptyCommit(t *testing.T) {
	src, wt, _, _, hotfix := setupCherryPickRepo(t, "cp-empty-commit")
	client := stubArgus(t, src, wt)
	_, err := CherryPick(context.Background(), CherryPickInput{
		Client: client, TaskID: "t", Commit: "", TargetBranch: hotfix,
	})
	if err == nil || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("expected error mentioning commit field, got: %v", err)
	}
}

func TestCherryPick_RefusesEmptyTargetBranch(t *testing.T) {
	src, wt, _, commit, _ := setupCherryPickRepo(t, "cp-empty-target")
	client := stubArgus(t, src, wt)
	_, err := CherryPick(context.Background(), CherryPickInput{
		Client: client, TaskID: "t", Commit: commit, TargetBranch: "",
	})
	if err == nil || !strings.Contains(err.Error(), "target_branch") {
		t.Fatalf("expected error mentioning target_branch field, got: %v", err)
	}
}

func TestCherryPick_RefusesLeadingDashCommit(t *testing.T) {
	src, wt, _, _, hotfix := setupCherryPickRepo(t, "cp-dash-commit")
	client := stubArgus(t, src, wt)
	_, err := CherryPick(context.Background(), CherryPickInput{
		Client: client, TaskID: "t", Commit: "--upload-pack=evil", TargetBranch: hotfix,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid commit") {
		t.Fatalf("expected 'invalid commit' error, got: %v", err)
	}
}

func TestCherryPick_RefusesLeadingDashTargetBranch(t *testing.T) {
	src, wt, _, commit, _ := setupCherryPickRepo(t, "cp-dash-target")
	client := stubArgus(t, src, wt)
	_, err := CherryPick(context.Background(), CherryPickInput{
		Client: client, TaskID: "t", Commit: commit, TargetBranch: "--upload-pack=evil",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid target_branch") {
		t.Fatalf("expected 'invalid target_branch' error, got: %v", err)
	}
}

func TestCherryPick_RefusesDefaultBranchTarget(t *testing.T) {
	src, wt, _, commit, _ := setupCherryPickRepo(t, "cp-default")
	client := stubArgus(t, src, wt)

	beforeBranch := currentBranchOrFail(t, src)
	beforeHead := headSHA(t, src)

	for _, name := range []string{"main", "master"} {
		_, err := CherryPick(context.Background(), CherryPickInput{
			Client: client, TaskID: "t", Commit: commit, TargetBranch: name,
		})
		if err == nil {
			t.Fatalf("expected refusal for target_branch=%q", name)
		}
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("error should name the refused branch %q: %v", name, err)
		}
	}
	// Source repo state must be untouched on a refusal.
	if currentBranchOrFail(t, src) != beforeBranch {
		t.Fatal("source repo branch changed on refused cherry-pick")
	}
	if headSHA(t, src) != beforeHead {
		t.Fatal("source repo HEAD changed on refused cherry-pick")
	}
}

func TestCherryPick_RefusesUnknownTargetBranch(t *testing.T) {
	src, wt, _, commit, _ := setupCherryPickRepo(t, "cp-unknown-target")
	client := stubArgus(t, src, wt)
	_, err := CherryPick(context.Background(), CherryPickInput{
		Client: client, TaskID: "t", Commit: commit, TargetBranch: "no-such-branch",
	})
	if err == nil {
		t.Fatal("expected error for unknown target branch")
	}
}

func TestCherryPick_RefusesUnresolvableCommit(t *testing.T) {
	src, wt, _, _, hotfix := setupCherryPickRepo(t, "cp-unresolvable")
	client := stubArgus(t, src, wt)
	_, err := CherryPick(context.Background(), CherryPickInput{
		Client: client, TaskID: "t", Commit: "deadbeef0123dead", TargetBranch: hotfix,
	})
	if err == nil {
		t.Fatal("expected error for unresolvable commit")
	}
}

func TestCherryPick_AbortsOnConflict(t *testing.T) {
	src, wt, _, _, hotfix := setupCherryPickRepo(t, "cp-conflict")
	g := gitRunner(t)
	client := stubArgus(t, src, wt)

	// Put a CONFLICTING file at the same path on the hotfix branch so the
	// worktree's add-feature.txt commit collides.
	g(src, "checkout", hotfix)
	if err := os.WriteFile(filepath.Join(src, "feature.txt"), []byte("DIFFERENT\n"), 0o644); err != nil {
		t.Fatalf("write conflicting feature.txt: %v", err)
	}
	g(src, "add", "feature.txt")
	g(src, "commit", "-m", "conflicting feature.txt on hotfix")
	g(src, "checkout", "main") // move back; cherry_pick will switch on its own
	conflictingHead := revParse(t, src, "refs/heads/"+hotfix)
	commit := strings.TrimSpace(g(wt, "rev-parse", "HEAD"))

	_, err := CherryPick(context.Background(), CherryPickInput{
		Client: client, TaskID: "t", Commit: commit, TargetBranch: hotfix,
	})
	if err == nil {
		t.Fatal("expected cherry-pick to fail on conflict")
	}
	// Error should carry a hint of the conflict path.
	if !strings.Contains(err.Error(), "feature.txt") {
		t.Fatalf("error should mention the conflicting path: %v", err)
	}
	// Source repo should be left on the target branch with a clean tree —
	// i.e., the abort succeeded.
	if branch := currentBranchOrFail(t, src); branch != hotfix {
		t.Fatalf("after abort, source repo should be on %s; on %s", hotfix, branch)
	}
	if got := revParse(t, src, "refs/heads/"+hotfix); got != conflictingHead {
		t.Fatalf("hotfix HEAD moved on aborted cherry-pick: got %q want %q", got, conflictingHead)
	}
	// No CHERRY_PICK_HEAD should remain.
	if _, err := os.Stat(filepath.Join(src, ".git", "CHERRY_PICK_HEAD")); err == nil {
		t.Fatal("CHERRY_PICK_HEAD still present after abort")
	}
	// Working tree should be clean.
	status := strings.TrimSpace(g(src, "status", "--porcelain"))
	if status != "" {
		t.Fatalf("working tree should be clean after abort; status:\n%s", status)
	}
}

func TestCherryPick_RefusesUnknownTask(t *testing.T) {
	client := stubArgusTaskNotFound(t)
	_, err := CherryPick(context.Background(), CherryPickInput{
		Client: client, TaskID: "ghost", Commit: "abc", TargetBranch: "feature/x",
	})
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected error naming task id, got: %v", err)
	}
}

func TestCherryPick_LockSerializesConcurrentCalls(t *testing.T) {
	src, wt, _, commit, hotfix := setupCherryPickRepo(t, "cp-lock")
	client := stubArgus(t, src, wt)

	canon, _ := filepath.EvalSymlinks(src)
	mu := lockSourceRepo(canon)
	released := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := CherryPick(context.Background(), CherryPickInput{
			Client: client, TaskID: "t", Commit: commit, TargetBranch: hotfix,
		})
		if err != nil {
			t.Errorf("cherry_pick under lock: %v", err)
		}
		close(released)
	}()
	select {
	case <-released:
		mu.Unlock()
		t.Fatal("cherry_pick did not block on held lock")
	case <-time.After(150 * time.Millisecond):
	}
	mu.Unlock()
	wg.Wait()
}

package verbs

import (
	"context"
	"strings"
	"testing"
)

func TestGHPRMerge_HappySquash(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghmerge-squash")
	client := stubArgus(t, src, wt)

	body := fakeGHCaptureArgv + `
echo "merged PR #7"
exit 0
`
	dir := writeFakeGH(t, body)

	result, err := GHPRMerge(context.Background(), client, "task-merge", GHPRMergeOptions{PRNumber: 7, Strategy: "squash"})
	if err != nil {
		t.Fatalf("gh pr merge: %v", err)
	}
	if !result.Merged {
		t.Fatal("expected Merged=true")
	}
	if result.Strategy != "squash" {
		t.Fatalf("strategy mismatch: %q", result.Strategy)
	}
	argv := readFakeGHArgv(t, dir)
	if !strings.Contains(argv, "--squash") {
		t.Fatalf("argv missing --squash:\n%s", argv)
	}
	if !strings.Contains(argv, "7") {
		t.Fatalf("argv missing PR number 7:\n%s", argv)
	}
}

func TestGHPRMerge_RebaseStrategy(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghmerge-rebase")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHCaptureArgv+"\nexit 0\n")

	_, err := GHPRMerge(context.Background(), client, "task-r", GHPRMergeOptions{PRNumber: 8, Strategy: "rebase"})
	if err != nil {
		t.Fatalf("gh pr merge: %v", err)
	}
	argv := readFakeGHArgv(t, dir)
	if !strings.Contains(argv, "--rebase") {
		t.Fatalf("argv missing --rebase:\n%s", argv)
	}
}

func TestGHPRMerge_MergeStrategy(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghmerge-merge")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHCaptureArgv+"\nexit 0\n")

	_, err := GHPRMerge(context.Background(), client, "task-m", GHPRMergeOptions{PRNumber: 9, Strategy: "merge"})
	if err != nil {
		t.Fatalf("gh pr merge: %v", err)
	}
	argv := readFakeGHArgv(t, dir)
	if !strings.Contains(argv, "--merge") {
		t.Fatalf("argv missing --merge:\n%s", argv)
	}
}

func TestGHPRMerge_GHErrorSurfacesStderr(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghmerge-err")
	client := stubArgus(t, src, wt)
	body := fakeGHCaptureArgv + `
echo "Pull request not mergeable: required checks have failed" 1>&2
exit 1
`
	writeFakeGH(t, body)

	_, err := GHPRMerge(context.Background(), client, "task-e", GHPRMergeOptions{PRNumber: 10, Strategy: "squash"})
	if err == nil {
		t.Fatal("expected error from gh non-zero exit, got nil")
	}
	if !strings.Contains(err.Error(), "required checks") {
		t.Fatalf("expected gh stderr in error, got: %v", err)
	}
}

func TestGHPRMerge_InvalidStrategyRejectedBeforeGH(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghmerge-bad-strat")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHCaptureArgv+"\nexit 0\n")

	_, err := GHPRMerge(context.Background(), client, "task-bad", GHPRMergeOptions{PRNumber: 1, Strategy: "rebase-squash"})
	if err == nil {
		t.Fatal("expected error for invalid strategy, got nil")
	}
	if !strings.Contains(err.Error(), "strategy") {
		t.Fatalf("expected strategy validation error, got: %v", err)
	}
	if argv := readFakeGHArgv(t, dir); argv != "" {
		t.Fatalf("expected gh NOT to be invoked, but argv was captured:\n%s", argv)
	}
}

func TestGHPRMerge_RefusesUnknownTaskID(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t)
	_, err := GHPRMerge(context.Background(), client, "ghost-task", GHPRMergeOptions{PRNumber: 1, Strategy: "squash"})
	if err == nil {
		t.Fatal("expected error for unknown task, got nil")
	}
	if !strings.Contains(err.Error(), "ghost-task") {
		t.Fatalf("expected error to name task id, got: %v", err)
	}
}

func TestGHPRMerge_RejectsNonPositivePR(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t) // never reached
	_, err := GHPRMerge(context.Background(), client, "task-x", GHPRMergeOptions{PRNumber: 0, Strategy: "squash"})
	if err == nil {
		t.Fatal("expected error for pr_number=0, got nil")
	}
	if !strings.Contains(err.Error(), "pr_number") {
		t.Fatalf("expected pr_number validation error, got: %v", err)
	}
}

func TestGHPRMerge_RestoresDefaultBranchWhenOnStrayBranch(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghmerge-restore")
	client := stubArgus(t, src, wt)

	// Put canonical source repo on a stray branch (not the worktree's branch,
	// not main). Simulates the state where a prior operation left the repo
	// on the wrong branch.
	g := gitRunner(t)
	g(src, "branch", "stray-old-feature")
	g(src, "checkout", "stray-old-feature")

	writeFakeGH(t, fakeGHCaptureArgv+"\nexit 0\n")

	result, err := GHPRMerge(context.Background(), client, "task-restore", GHPRMergeOptions{PRNumber: 1, Strategy: "squash"})
	if err != nil {
		t.Fatalf("gh pr merge: %v", err)
	}
	if !result.Merged {
		t.Fatal("expected Merged=true")
	}
	if result.DefaultBranch != "main" {
		t.Fatalf("expected DefaultBranch=main, got %q", result.DefaultBranch)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got: %v", result.Warnings)
	}

	// Canonical source repo must be back on main.
	branch, err := currentBranch(context.Background(), src)
	if err != nil {
		t.Fatalf("currentBranch: %v", err)
	}
	if branch != "main" {
		t.Fatalf("expected source repo on main after merge, got %q", branch)
	}
}

func TestGHPRMerge_PullsWhenAlreadyOnDefaultBranch(t *testing.T) {
	// When the source repo is already on main, GHPRMerge should pull and produce no warnings.
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghmerge-pull")
	client := stubArgus(t, src, wt)

	writeFakeGH(t, fakeGHCaptureArgv+"\nexit 0\n")

	// src is already on main (setupRepoWithBareAndWorktree leaves it there).
	result, err := GHPRMerge(context.Background(), client, "task-pull", GHPRMergeOptions{PRNumber: 2, Strategy: "squash"})
	if err != nil {
		t.Fatalf("gh pr merge: %v", err)
	}
	if !result.Merged {
		t.Fatal("expected Merged=true")
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got: %v", result.Warnings)
	}

	branch, err := currentBranch(context.Background(), src)
	if err != nil {
		t.Fatalf("currentBranch: %v", err)
	}
	if branch != "main" {
		t.Fatalf("expected source repo on main, got %q", branch)
	}
}

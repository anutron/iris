package verbs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupIntegrationBranchRepo builds a bare origin, a clone (src), and a
// long-lived non-default "target" branch already pushed to origin, plus a
// separate "source" branch with its own commit ready to be merged in. src's
// checkout is left on "source" so tests can assert merge_to_branch never
// disturbs it.
func setupIntegrationBranchRepo(t *testing.T, slug string) (src, bare, targetBranch, sourceBranch string) {
	t.Helper()
	tmp := t.TempDir()
	bare = filepath.Join(tmp, "origin.git")
	src = filepath.Join(tmp, "src")
	g := gitRunner(t)

	g("", "init", "--bare", "-b", "main", bare)
	g("", "clone", bare, src)
	g(src, "config", "user.email", "iris-test@example.com")
	g(src, "config", "user.name", "iris-test")
	g(src, "commit", "--allow-empty", "-m", "initial")
	g(src, "push", "-u", "origin", "main")
	g(src, "remote", "set-head", "origin", "main")

	targetBranch = "integration/" + slug
	g(src, "branch", targetBranch)
	g(src, "push", "-u", "origin", targetBranch)

	sourceBranch = "feature-" + slug
	g(src, "switch", "-c", sourceBranch)
	writeFileT(t, src, "feature.txt", "hello from "+sourceBranch+"\n")
	g(src, "add", "feature.txt")
	g(src, "commit", "-m", "add feature.txt on "+sourceBranch)
	g(src, "push", "-u", "origin", sourceBranch)

	return src, bare, targetBranch, sourceBranch
}

func writeFileT(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func worktreeListCount(t *testing.T, repo string) int {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("worktree list: %v\n%s", err, out)
	}
	// Each worktree entry starts with a "worktree <path>" line.
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			count++
		}
	}
	return count
}

// Delta scenario: "Successful merge into a long-lived integration branch."
func TestMergeToBranch_HappyPath(t *testing.T) {
	t.Parallel()
	src, bare, target, sourceBranch := setupIntegrationBranchRepo(t, "happy")
	client := stubArgus(t, src, src)

	result, err := MergeToBranch(context.Background(), client, "task-mtb-happy", target, sourceBranch, MergeOptions{NoFF: true})
	if err != nil {
		t.Fatalf("merge to branch: %v", err)
	}
	if result.TargetBranch != target {
		t.Fatalf("unexpected target branch: %q", result.TargetBranch)
	}
	if result.SourceRef != sourceBranch {
		t.Fatalf("unexpected source ref: %q", result.SourceRef)
	}
	if !result.Pushed {
		t.Fatal("expected pushed=true")
	}
	if result.SHA == "" {
		t.Fatal("expected non-empty merge sha")
	}
	remoteSHA := remoteRef(t, bare, target)
	if remoteSHA != result.SHA {
		t.Fatalf("origin/%s = %q, want merge sha %q", target, remoteSHA, result.SHA)
	}
}

// Delta scenario: "Source repo's checkout is never disturbed."
func TestMergeToBranch_SourceRepoCheckoutUndisturbed(t *testing.T) {
	t.Parallel()
	src, _, target, sourceBranch := setupIntegrationBranchRepo(t, "undisturbed")
	client := stubArgus(t, src, src)

	beforeBranch, err := exec.Command("git", "-C", src, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	beforeSHA := headSHA(t, src)

	if _, err := MergeToBranch(context.Background(), client, "task-mtb-undisturbed", target, sourceBranch, MergeOptions{NoFF: true}); err != nil {
		t.Fatalf("merge to branch: %v", err)
	}

	afterBranch, err := exec.Command("git", "-C", src, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	afterSHA := headSHA(t, src)

	if strings.TrimSpace(string(beforeBranch)) != strings.TrimSpace(string(afterBranch)) {
		t.Fatalf("checkout branch changed: before=%q after=%q", beforeBranch, afterBranch)
	}
	if beforeSHA != afterSHA {
		t.Fatalf("checkout HEAD changed: before=%q after=%q", beforeSHA, afterSHA)
	}
}

// Delta scenario: "Scratch worktree is always cleaned up" (success path).
func TestMergeToBranch_ScratchWorktreeRemovedOnSuccess(t *testing.T) {
	t.Parallel()
	src, _, target, sourceBranch := setupIntegrationBranchRepo(t, "cleanup-ok")
	client := stubArgus(t, src, src)

	before := worktreeListCount(t, src)
	if _, err := MergeToBranch(context.Background(), client, "task-mtb-cleanup-ok", target, sourceBranch, MergeOptions{NoFF: true}); err != nil {
		t.Fatalf("merge to branch: %v", err)
	}
	after := worktreeListCount(t, src)
	if after != before {
		t.Fatalf("expected worktree count unchanged after success: before=%d after=%d", before, after)
	}
}

// Delta scenario: "Scratch worktree is always cleaned up" (conflict path).
func TestMergeToBranch_ScratchWorktreeRemovedOnConflict(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "origin.git")
	src := filepath.Join(tmp, "src")
	g := gitRunner(t)

	g("", "init", "--bare", "-b", "main", bare)
	g("", "clone", bare, src)
	g(src, "config", "user.email", "x@y.z")
	g(src, "config", "user.name", "x")
	writeFileT(t, src, "f.txt", "base\n")
	g(src, "add", "f.txt")
	g(src, "commit", "-m", "initial")
	g(src, "push", "-u", "origin", "main")
	g(src, "remote", "set-head", "origin", "main")

	target := "integration/conflict"
	g(src, "branch", target)
	g(src, "push", "-u", "origin", target)

	// Advance target with a conflicting edit.
	g(src, "switch", target)
	writeFileT(t, src, "f.txt", "target-edit\n")
	g(src, "add", "f.txt")
	g(src, "commit", "-m", "target edit")
	g(src, "push", "origin", target)

	// Source branch, off main, with a conflicting edit to the same line.
	g(src, "switch", "main")
	sourceBranch := "feature-conflict"
	g(src, "switch", "-c", sourceBranch)
	writeFileT(t, src, "f.txt", "source-edit\n")
	g(src, "add", "f.txt")
	g(src, "commit", "-m", "source edit")
	g(src, "push", "-u", "origin", sourceBranch)

	client := stubArgus(t, src, src)
	before := worktreeListCount(t, src)
	remoteBefore := remoteRef(t, bare, target)

	_, err := MergeToBranch(context.Background(), client, "task-mtb-conflict", target, sourceBranch, MergeOptions{NoFF: true})
	if err == nil {
		t.Fatal("expected merge conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "merge") {
		t.Fatalf("unexpected error: %v", err)
	}
	after := worktreeListCount(t, src)
	if after != before {
		t.Fatalf("expected worktree count unchanged after conflict: before=%d after=%d", before, after)
	}
	if remoteAfter := remoteRef(t, bare, target); remoteAfter != remoteBefore {
		t.Fatalf("expected origin/%s unchanged after conflict: before=%q after=%q", target, remoteBefore, remoteAfter)
	}
}

// Delta scenario: "target_branch not tracking origin is reconciled before merging."
func TestMergeToBranch_ReconcilesStaleLocalTargetRef(t *testing.T) {
	t.Parallel()
	src, bare, target, sourceBranch := setupIntegrationBranchRepo(t, "stale")

	// Simulate another actor pushing directly to origin/target without src's
	// local ref (or its remote-tracking ref) knowing about it: clone fresh,
	// advance target there, push. src's local "target" branch ref remains at
	// the original commit until merge_to_branch fetches.
	tmp := t.TempDir()
	other := filepath.Join(tmp, "other")
	g := gitRunner(t)
	g("", "clone", bare, other)
	g(other, "config", "user.email", "x@y.z")
	g(other, "config", "user.name", "x")
	g(other, "switch", target)
	writeFileT(t, other, "upstream-advance.txt", "moved ahead\n")
	g(other, "add", "upstream-advance.txt")
	g(other, "commit", "-m", "advance target directly on origin")
	g(other, "push", "origin", target)

	client := stubArgus(t, src, src)
	result, err := MergeToBranch(context.Background(), client, "task-mtb-stale", target, sourceBranch, MergeOptions{NoFF: true})
	if err != nil {
		t.Fatalf("merge to branch (stale local ref must be reconciled): %v", err)
	}
	// If the scratch worktree hadn't reset to origin/target before merging,
	// the push would be rejected as non-fast-forward and we'd never get here.
	remoteSHA := remoteRef(t, bare, target)
	if remoteSHA != result.SHA {
		t.Fatalf("origin/%s = %q, want merge sha %q", target, remoteSHA, result.SHA)
	}
	// The merge commit's tree must contain the file only "other" introduced,
	// proving the merge based on the up-to-date origin tip, not the stale
	// local ref.
	out, err := exec.Command("git", "-C", src, "show", result.SHA+":upstream-advance.txt").CombinedOutput()
	if err != nil {
		t.Fatalf("expected merge commit to descend from origin's advanced tip: %v\n%s", err, out)
	}
}

// Delta scenario: "target_branch not yet on origin is merged from local state."
func TestMergeToBranch_TargetNotYetOnOrigin(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "origin.git")
	src := filepath.Join(tmp, "src")
	g := gitRunner(t)

	g("", "init", "--bare", "-b", "main", bare)
	g("", "clone", bare, src)
	g(src, "config", "user.email", "x@y.z")
	g(src, "config", "user.name", "x")
	g(src, "commit", "--allow-empty", "-m", "initial")
	g(src, "push", "-u", "origin", "main")
	g(src, "remote", "set-head", "origin", "main")

	target := "integration/never-pushed"
	g(src, "branch", target) // local only, never pushed

	sourceBranch := "feature-neverpushed"
	g(src, "switch", "-c", sourceBranch)
	writeFileT(t, src, "feature.txt", "hi\n")
	g(src, "add", "feature.txt")
	g(src, "commit", "-m", "feature commit")
	g(src, "push", "-u", "origin", sourceBranch)

	client := stubArgus(t, src, src)
	result, err := MergeToBranch(context.Background(), client, "task-mtb-neverpushed", target, sourceBranch, MergeOptions{NoFF: true})
	if err != nil {
		t.Fatalf("merge to branch: %v", err)
	}
	remoteSHA := remoteRef(t, bare, target)
	if remoteSHA == "" {
		t.Fatal("expected origin/target to now exist")
	}
	if remoteSHA != result.SHA {
		t.Fatalf("origin/%s = %q, want merge sha %q", target, remoteSHA, result.SHA)
	}
}

// Delta scenario: "Refuses empty target_branch or source_ref."
func TestMergeToBranch_RefusesEmptyArgs(t *testing.T) {
	t.Parallel()
	src, _, target, sourceBranch := setupIntegrationBranchRepo(t, "empty-args")
	client := stubArgus(t, src, src)

	if _, err := MergeToBranch(context.Background(), client, "task-mtb-empty1", "", sourceBranch, MergeOptions{NoFF: true}); err == nil {
		t.Fatal("expected error for empty target_branch, got nil")
	}
	if _, err := MergeToBranch(context.Background(), client, "task-mtb-empty2", target, "", MergeOptions{NoFF: true}); err == nil {
		t.Fatal("expected error for empty source_ref, got nil")
	}
}

// Delta scenario: "Refuses a target_branch or source_ref beginning with a dash."
func TestMergeToBranch_RefusesLeadingDash(t *testing.T) {
	t.Parallel()
	src, _, target, sourceBranch := setupIntegrationBranchRepo(t, "dash-args")
	client := stubArgus(t, src, src)

	_, err := MergeToBranch(context.Background(), client, "task-mtb-dash1", "--upload-pack=evil", sourceBranch, MergeOptions{NoFF: true})
	if err == nil || !strings.Contains(err.Error(), "must not begin with '-'") {
		t.Fatalf("expected leading-dash target_branch error, got: %v", err)
	}
	_, err = MergeToBranch(context.Background(), client, "task-mtb-dash2", target, "--upload-pack=evil", MergeOptions{NoFF: true})
	if err == nil || !strings.Contains(err.Error(), "must not begin with '-'") {
		t.Fatalf("expected leading-dash source_ref error, got: %v", err)
	}
}

// Delta scenario: "Refuses merging a branch into itself."
func TestMergeToBranch_RefusesSelfMerge(t *testing.T) {
	t.Parallel()
	src, _, target, _ := setupIntegrationBranchRepo(t, "self-merge")
	client := stubArgus(t, src, src)

	_, err := MergeToBranch(context.Background(), client, "task-mtb-self", target, target, MergeOptions{NoFF: true})
	if err == nil {
		t.Fatal("expected error refusing self-merge, got nil")
	}
	if !strings.Contains(err.Error(), "itself") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Delta scenario: "Refuses targeting the default or protected branch."
func TestMergeToBranch_RefusesDefaultBranchTarget(t *testing.T) {
	t.Parallel()
	src, _, _, sourceBranch := setupIntegrationBranchRepo(t, "default-target")
	client := stubArgus(t, src, src)

	for _, target := range []string{"main", "master"} {
		_, err := MergeToBranch(context.Background(), client, "task-mtb-defaulttarget", target, sourceBranch, MergeOptions{NoFF: true})
		if err == nil {
			t.Fatalf("expected error refusing target_branch=%q, got nil", target)
		}
		if !strings.Contains(err.Error(), "merge_to_master") {
			t.Fatalf("expected error to redirect to iris_merge_to_master, got: %v", err)
		}
	}
}

// Delta scenario: "no_ff=false allows fast-forward."
func TestMergeToBranch_FastForwardWhenNoFFFalse(t *testing.T) {
	t.Parallel()
	src, _, target, sourceBranch := setupIntegrationBranchRepo(t, "ff")
	client := stubArgus(t, src, src)

	// sourceBranch is a strict descendant of target's origin state in this
	// fixture (target has no commits main didn't already have), so ff-only
	// succeeds.
	result, err := MergeToBranch(context.Background(), client, "task-mtb-ff", target, sourceBranch, MergeOptions{NoFF: false})
	if err != nil {
		t.Fatalf("ff merge to branch: %v", err)
	}
	if result.SHA == "" {
		t.Fatal("expected non-empty sha")
	}
	out, err := exec.Command("git", "-C", src, "log", "--oneline", "-3", "--no-merges").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "Merge branch") {
		t.Fatalf("--ff-only produced a merge commit (should be linear):\n%s", out)
	}
}

// Delta scenario: "Custom merge message."
func TestMergeToBranch_CustomMessage(t *testing.T) {
	t.Parallel()
	src, _, target, sourceBranch := setupIntegrationBranchRepo(t, "message")
	client := stubArgus(t, src, src)

	const subject = "integrate: my custom subject"
	result, err := MergeToBranch(context.Background(), client, "task-mtb-message", target, sourceBranch, MergeOptions{NoFF: true, Message: subject})
	if err != nil {
		t.Fatalf("merge to branch: %v", err)
	}
	out, err := exec.Command("git", "-C", src, "log", "-1", "--format=%s", result.SHA).CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != subject {
		t.Fatalf("merge subject: got %q, want %q", strings.TrimSpace(string(out)), subject)
	}
}

// Delta scenario: "Dry-run previews a clean merge."
func TestMergeToBranch_DryRunCleanPreview(t *testing.T) {
	t.Parallel()
	src, bare, target, sourceBranch := setupIntegrationBranchRepo(t, "dry-clean")
	client := stubArgus(t, src, src)
	remoteBefore := remoteRef(t, bare, target)

	result, err := MergeToBranch(context.Background(), client, "task-mtb-dryclean", target, sourceBranch, MergeOptions{NoFF: true, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run merge: %v", err)
	}
	if !result.DryRun {
		t.Fatal("expected dry_run=true")
	}
	if !result.WouldSucceed {
		t.Fatalf("expected would_succeed=true, conflicts=%v", result.Conflicts)
	}
	if result.SHA != "" {
		t.Fatalf("expected empty sha on dry-run, got %q", result.SHA)
	}
	if !contains(result.FilesChanged, "feature.txt") {
		t.Fatalf("expected files_changed to include feature.txt, got %v", result.FilesChanged)
	}
	if remoteAfter := remoteRef(t, bare, target); remoteAfter != remoteBefore {
		t.Fatalf("dry-run must not push: before=%q after=%q", remoteBefore, remoteAfter)
	}
}

// Delta scenario: "Dry-run previews a conflicted merge."
func TestMergeToBranch_DryRunConflictPreview(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "origin.git")
	src := filepath.Join(tmp, "src")
	g := gitRunner(t)

	g("", "init", "--bare", "-b", "main", bare)
	g("", "clone", bare, src)
	g(src, "config", "user.email", "x@y.z")
	g(src, "config", "user.name", "x")
	writeFileT(t, src, "f.txt", "base\n")
	g(src, "add", "f.txt")
	g(src, "commit", "-m", "initial")
	g(src, "push", "-u", "origin", "main")
	g(src, "remote", "set-head", "origin", "main")

	target := "integration/dry-conflict"
	g(src, "branch", target)
	g(src, "push", "-u", "origin", target)
	g(src, "switch", target)
	writeFileT(t, src, "f.txt", "target-edit\n")
	g(src, "add", "f.txt")
	g(src, "commit", "-m", "target edit")
	g(src, "push", "origin", target)

	g(src, "switch", "main")
	sourceBranch := "feature-dry-conflict"
	g(src, "switch", "-c", sourceBranch)
	writeFileT(t, src, "f.txt", "source-edit\n")
	g(src, "add", "f.txt")
	g(src, "commit", "-m", "source edit")
	g(src, "push", "-u", "origin", sourceBranch)

	client := stubArgus(t, src, src)
	result, err := MergeToBranch(context.Background(), client, "task-mtb-dryconflict", target, sourceBranch, MergeOptions{NoFF: true, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run merge (should report conflict in result, not error): %v", err)
	}
	if result.WouldSucceed {
		t.Fatalf("expected would_succeed=false, conflicts=%v", result.Conflicts)
	}
	if !contains(result.Conflicts, "f.txt") {
		t.Fatalf("expected f.txt in conflicts, got %v", result.Conflicts)
	}
}

// Delta scenario: "Dry-run skips post_merge hook and push."
func TestMergeToBranch_DryRunSkipsPostMergeHook(t *testing.T) {
	t.Parallel()
	src, bare, target, sourceBranch := setupIntegrationBranchRepo(t, "dry-hook")
	writeIrisTomlPostMergeOnBranch(t, src, target, []string{"sh", "-c", "echo ran > " + filepath.Join(t.TempDir(), "marker.txt")})

	client := stubArgus(t, src, src)
	remoteBefore := remoteRef(t, bare, target)

	result, err := MergeToBranch(context.Background(), client, "task-mtb-dryhook", target, sourceBranch, MergeOptions{NoFF: true, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run merge: %v", err)
	}
	if result.PostMerge != nil {
		t.Fatalf("expected post_merge=nil on dry-run, got %+v", result.PostMerge)
	}
	if remoteAfter := remoteRef(t, bare, target); remoteAfter != remoteBefore {
		t.Fatalf("dry-run must not push: before=%q after=%q", remoteBefore, remoteAfter)
	}
}

// Delta scenario: "post_merge hook runs from the merged branch's tree after a
// successful push" + "post_merge hook reads .iris.toml from the merged tree,
// not the source repo's current checkout."
func TestMergeToBranch_PostMergeHookRunsFromMergedTree(t *testing.T) {
	t.Parallel()
	src, _, target, sourceBranch := setupIntegrationBranchRepo(t, "hook-happy")
	envDump := filepath.Join(t.TempDir(), "env.txt")
	writeIrisTomlPostMergeOnBranch(t, src, target, []string{"sh", "-c",
		"echo task_id=$IRIS_TASK_ID > " + envDump +
			" && echo source_repo=$IRIS_SOURCE_REPO >> " + envDump +
			" && echo target_branch=$IRIS_TARGET_BRANCH >> " + envDump +
			" && echo source_ref=$IRIS_SOURCE_REF >> " + envDump +
			" && echo merge_sha=$IRIS_MERGE_SHA >> " + envDump +
			" && echo hello"})
	// The source repo's OWN checked-out branch (sourceBranch, per
	// setupIntegrationBranchRepo) must have no .iris.toml, proving the hook
	// came from target_branch's tree, not the current checkout.
	if _, err := os.Stat(filepath.Join(src, ".iris.toml")); !os.IsNotExist(err) {
		t.Fatalf("expected no .iris.toml on current checkout before merge: %v", err)
	}

	client := stubArgus(t, src, src)
	result, err := MergeToBranch(context.Background(), client, "task-mtb-hookhappy", target, sourceBranch, MergeOptions{NoFF: true})
	if err != nil {
		t.Fatalf("merge to branch: %v", err)
	}
	if result.PostMerge == nil {
		t.Fatal("expected post_merge result")
	}
	if result.PostMerge.ExitCode != 0 {
		t.Fatalf("expected exit_code=0, got %+v", result.PostMerge)
	}
	if !strings.Contains(result.PostMerge.Stdout, "hello") {
		t.Fatalf("expected stdout to capture echo, got %q", result.PostMerge.Stdout)
	}

	body, err := os.ReadFile(envDump)
	if err != nil {
		t.Fatalf("read env dump: %v", err)
	}
	got := string(body)
	wantSrc, _ := filepath.EvalSymlinks(src)
	checks := map[string]string{
		"task_id":       "task-mtb-hookhappy",
		"source_repo":   wantSrc,
		"target_branch": target,
		"source_ref":    sourceBranch,
		"merge_sha":     result.SHA,
	}
	for k, v := range checks {
		if !strings.Contains(got, k+"="+v) {
			t.Fatalf("env dump missing %s=%s; full:\n%s", k, v, got)
		}
	}
}

// Delta scenario: "post_merge failure does not roll back the merge or push."
func TestMergeToBranch_PostMergeFailureDoesNotRollback(t *testing.T) {
	t.Parallel()
	src, bare, target, sourceBranch := setupIntegrationBranchRepo(t, "hook-fail")
	writeIrisTomlPostMergeOnBranch(t, src, target, []string{"sh", "-c", "echo oops >&2; exit 7"})

	client := stubArgus(t, src, src)
	result, err := MergeToBranch(context.Background(), client, "task-mtb-hookfail", target, sourceBranch, MergeOptions{NoFF: true})
	if err != nil {
		t.Fatalf("merge to branch: %v (post_merge failure must NOT propagate as Go error)", err)
	}
	if result.PostMerge == nil || result.PostMerge.ExitCode != 7 {
		t.Fatalf("expected exit_code=7, got %+v", result.PostMerge)
	}
	remoteSHA := remoteRef(t, bare, target)
	if remoteSHA != result.SHA {
		t.Fatalf("expected merge to remain pushed despite hook failure: origin=%q sha=%q", remoteSHA, result.SHA)
	}
}

// Delta scenario: "Missing .iris.toml does not block merge."
func TestMergeToBranch_MissingIrisTomlDoesNotBlock(t *testing.T) {
	t.Parallel()
	src, _, target, sourceBranch := setupIntegrationBranchRepo(t, "no-toml")
	client := stubArgus(t, src, src)

	result, err := MergeToBranch(context.Background(), client, "task-mtb-notoml", target, sourceBranch, MergeOptions{NoFF: true})
	if err != nil {
		t.Fatalf("merge to branch: %v", err)
	}
	if result.PostMerge != nil {
		t.Fatalf("expected post_merge=nil when .iris.toml absent, got %+v", result.PostMerge)
	}
}

// Delta scenario: "Arbitrary source_ref types are accepted" (tag).
func TestMergeToBranch_SourceRefAsTag(t *testing.T) {
	t.Parallel()
	src, bare, target, sourceBranch := setupIntegrationBranchRepo(t, "tag-ref")
	g := gitRunner(t)
	g(src, "tag", "v-mtb-tag", sourceBranch)
	g(src, "push", "origin", "v-mtb-tag")

	client := stubArgus(t, src, src)
	result, err := MergeToBranch(context.Background(), client, "task-mtb-tagref", target, "v-mtb-tag", MergeOptions{NoFF: true})
	if err != nil {
		t.Fatalf("merge to branch (tag source_ref): %v", err)
	}
	remoteSHA := remoteRef(t, bare, target)
	if remoteSHA != result.SHA {
		t.Fatalf("origin/%s = %q, want merge sha %q", target, remoteSHA, result.SHA)
	}
}

// Delta scenario: "Arbitrary source_ref types are accepted" (raw SHA).
func TestMergeToBranch_SourceRefAsSHA(t *testing.T) {
	t.Parallel()
	src, bare, target, sourceBranch := setupIntegrationBranchRepo(t, "sha-ref")
	sha := headSHA(t, src) // src is currently on sourceBranch's tip
	_ = sourceBranch

	client := stubArgus(t, src, src)
	result, err := MergeToBranch(context.Background(), client, "task-mtb-sharef", target, sha, MergeOptions{NoFF: true})
	if err != nil {
		t.Fatalf("merge to branch (SHA source_ref): %v", err)
	}
	remoteSHA := remoteRef(t, bare, target)
	if remoteSHA != result.SHA {
		t.Fatalf("origin/%s = %q, want merge sha %q", target, remoteSHA, result.SHA)
	}
}

// Host-bridge scenario: "Refuses an unknown task ID."
func TestMergeToBranch_RefusesUnknownTaskID(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t)
	_, err := MergeToBranch(context.Background(), client, "ghost-task", "integration/x", "feature-x", MergeOptions{NoFF: true})
	if err == nil {
		t.Fatal("expected error for unknown task, got nil")
	}
	if !strings.Contains(err.Error(), "ghost-task") {
		t.Fatalf("expected error to name task id, got: %v", err)
	}
}

// writeIrisTomlPostMergeOnBranch writes a minimal .iris.toml with a
// [post_merge] block on targetBranch specifically (not on whatever branch
// the source repo currently has checked out), then pushes it, so tests can
// prove the hook is read from the merged tree.
func writeIrisTomlPostMergeOnBranch(t *testing.T, srcRepo, targetBranch string, command []string) {
	t.Helper()
	g := gitRunner(t)
	currentBranch, err := exec.Command("git", "-C", srcRepo, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	g(srcRepo, "switch", targetBranch)

	var b strings.Builder
	b.WriteString("schema_version = 1\n\n[build]\ncommand = [\"true\"]\n\n[restart]\nmechanism = \"none\"\n\n[post_merge]\ncommand = [")
	for i, c := range command {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("\"")
		b.WriteString(strings.ReplaceAll(c, `"`, `\"`))
		b.WriteString("\"")
	}
	b.WriteString("]\n")

	writeFileT(t, srcRepo, ".iris.toml", b.String())
	g(srcRepo, "add", ".iris.toml")
	g(srcRepo, "commit", "-m", "add iris.toml on "+targetBranch)
	g(srcRepo, "push", "origin", targetBranch)

	g(srcRepo, "switch", strings.TrimSpace(string(currentBranch)))
}

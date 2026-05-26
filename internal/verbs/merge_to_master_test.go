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

	"github.com/anutron/iris/internal/argus"
)

// stubArgus returns an httptest.Server that answers GET /api/tasks/<id>
// with a fixed Task pointing at worktreePath. Callers run a test git
// scenario in the temp worktree, then ask iris to merge it.
func stubArgus(t *testing.T, worktreePath string) *argus.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/tasks/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":            strings.TrimPrefix(r.URL.Path, "/api/tasks/"),
			"name":          "stub",
			"project":       "iris-test",
			"status":        "in_progress",
			"worktree_path": worktreePath,
		})
	}))
	t.Cleanup(srv.Close)
	return argus.New(srv.URL, "stub-token")
}

// setupRepoWithWorktree creates a realistic scenario: a bare "origin"
// repo, a "source" clone with one commit on main, and an argus/<slug>
// branch checked out as a linked worktree with its own commit. Returns
// (sourceRepo, worktreePath).
func setupRepoWithWorktree(t *testing.T, slug string) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "origin.git")
	src := filepath.Join(tmp, "src")
	wt := filepath.Join(tmp, "wt")

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		if dir != "" {
			cmd.Dir = dir
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s (cwd=%s): %v\n%s", strings.Join(args, " "), dir, err, out)
		}
	}

	run("", "init", "--bare", "-b", "main", bare)
	run("", "clone", bare, src)
	run(src, "config", "user.email", "iris-test@example.com")
	run(src, "config", "user.name", "iris-test")
	run(src, "commit", "--allow-empty", "-m", "initial")
	run(src, "push", "-u", "origin", "main")
	// Create the argus branch and add it as a linked worktree.
	branch := "argus/" + slug
	run(src, "branch", branch)
	run(src, "worktree", "add", wt, branch)
	// Make a unique commit on the worktree branch so the merge has content.
	run(wt, "config", "user.email", "iris-test@example.com")
	run(wt, "config", "user.name", "iris-test")
	run(wt, "commit", "--allow-empty", "-m", "work on "+branch)
	return src, wt
}

func TestMergeToMaster_RefusesNonArgusBranch(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	wt := filepath.Join(tmp, "wt")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		if dir != "" {
			cmd.Dir = dir
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("", "init", "-b", "main", src)
	run(src, "config", "user.email", "x@y.z")
	run(src, "config", "user.name", "x")
	run(src, "commit", "--allow-empty", "-m", "initial")
	run(src, "branch", "feature/something")
	run(src, "worktree", "add", wt, "feature/something")

	client := stubArgus(t, wt)
	_, err := MergeToMaster(context.Background(), client, "task-1", MergeOptions{NoFF: true})
	if err == nil {
		t.Fatal("expected error for non-argus branch, got nil")
	}
	if !strings.Contains(err.Error(), "non-argus branch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMergeToMaster_HappyPath(t *testing.T) {
	src, wt := setupRepoWithWorktree(t, "happy-slug")
	client := stubArgus(t, wt)

	result, err := MergeToMaster(context.Background(), client, "task-happy", MergeOptions{NoFF: true})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if result.TaskBranch != "argus/happy-slug" {
		t.Fatalf("unexpected branch: %q", result.TaskBranch)
	}
	if result.DefaultBranch != "main" {
		t.Fatalf("unexpected default branch: %q", result.DefaultBranch)
	}
	// macOS prefixes the temp dir with /private; canonicalize for comparison.
	wantSrc, _ := filepath.EvalSymlinks(src)
	if result.SourceRepo != wantSrc {
		t.Fatalf("unexpected source repo: %q (want %q)", result.SourceRepo, wantSrc)
	}
	if result.SHA == "" {
		t.Fatal("empty merge SHA")
	}
	// Verify the source repo is now on main with a merge commit.
	out, err := exec.Command("git", "-C", src, "log", "--oneline", "-2").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Merge branch 'argus/happy-slug'") {
		t.Fatalf("merge commit subject missing:\n%s", out)
	}
}

func TestMergeToMaster_ConflictAborts(t *testing.T) {
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "origin.git")
	src := filepath.Join(tmp, "src")
	wt := filepath.Join(tmp, "wt")

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		if dir != "" {
			cmd.Dir = dir
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s (cwd=%s): %v\n%s", strings.Join(args, " "), dir, err, out)
		}
	}
	writeFile := func(dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	run("", "init", "--bare", "-b", "main", bare)
	run("", "clone", bare, src)
	run(src, "config", "user.email", "x@y.z")
	run(src, "config", "user.name", "x")
	writeFile(src, "f.txt", "from-main\n")
	run(src, "add", "f.txt")
	run(src, "commit", "-m", "initial main")
	run(src, "push", "-u", "origin", "main")
	run(src, "branch", "argus/conflict-slug")
	run(src, "worktree", "add", wt, "argus/conflict-slug")
	// Diverge: edit f.txt on main…
	writeFile(src, "f.txt", "main-edit\n")
	run(src, "add", "f.txt")
	run(src, "commit", "-m", "main edit")
	run(src, "push", "origin", "main")
	// …and conflictingly edit it on the worktree.
	writeFile(wt, "f.txt", "wt-edit\n")
	run(wt, "config", "user.email", "x@y.z")
	run(wt, "config", "user.name", "x")
	run(wt, "add", "f.txt")
	run(wt, "commit", "-m", "wt edit")

	client := stubArgus(t, wt)
	_, err := MergeToMaster(context.Background(), client, "task-conflict", MergeOptions{NoFF: true})
	if err == nil {
		t.Fatal("expected merge conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "merge") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Source repo should be back on main with no in-progress merge.
	out, _ := exec.Command("git", "-C", src, "status", "--porcelain=v2", "--branch").CombinedOutput()
	if strings.Contains(string(out), "MERGE_HEAD") {
		t.Fatalf("expected merge aborted, but MERGE_HEAD present:\n%s", out)
	}
}

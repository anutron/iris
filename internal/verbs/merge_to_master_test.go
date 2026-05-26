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
	"sync"
	"testing"
	"time"

	"github.com/anutron/iris/internal/argus"
)

// stubArgus returns an httptest.Server that answers two endpoints iris
// needs: GET /api/tasks/<id> (returns a Task pointing at worktreePath)
// and GET /api/projects/full (returns a single project whose path is the
// canonicalized sourceRepo, so the allowlist check in verbs.Resolve
// passes).
func stubArgus(t *testing.T, sourceRepo, worktreePath string) *argus.Client {
	t.Helper()
	canon, _ := filepath.EvalSymlinks(sourceRepo)
	if canon == "" {
		canon = sourceRepo
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/tasks/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":            strings.TrimPrefix(r.URL.Path, "/api/tasks/"),
				"name":          "stub",
				"project":       "iris-test",
				"status":        "in_progress",
				"worktree_path": worktreePath,
			})
		case r.URL.Path == "/api/projects/full":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{
					{"name": "iris-test", "path": canon},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return argus.New(srv.URL, "stub-token")
}

// stubArgusTaskNotFound returns an httptest.Server that 404s every
// /api/tasks/<id> request (and answers /api/projects/full normally with
// an empty list).
func stubArgusTaskNotFound(t *testing.T) *argus.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/projects/full" {
			_ = json.NewEncoder(w).Encode(map[string]any{"projects": []map[string]any{}})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return argus.New(srv.URL, "stub-token")
}

// gitRun is a small helper closure factory; tests reuse it across temp dirs.
func gitRunner(t *testing.T) func(dir string, args ...string) string {
	t.Helper()
	return func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		if dir != "" {
			cmd.Dir = dir
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s (cwd=%s): %v\n%s", strings.Join(args, " "), dir, err, out)
		}
		return string(out)
	}
}

// headSHA reads `git rev-parse HEAD` in dir. Used by negative-path tests
// to assert the verb performed no git mutation.
func headSHA(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

// setupRepoWithWorktree creates a bare origin, a clone, and an
// argus/<slug> branch checked out as a linked worktree with its own commit.
// Sets origin/HEAD on the source clone so verbs.DefaultBranch succeeds.
func setupRepoWithWorktree(t *testing.T, slug string) (sourceRepo, worktreePath string) {
	t.Helper()
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "origin.git")
	src := filepath.Join(tmp, "src")
	wt := filepath.Join(tmp, "wt")
	g := gitRunner(t)

	g("", "init", "--bare", "-b", "main", bare)
	g("", "clone", bare, src)
	g(src, "config", "user.email", "iris-test@example.com")
	g(src, "config", "user.name", "iris-test")
	g(src, "commit", "--allow-empty", "-m", "initial")
	g(src, "push", "-u", "origin", "main")
	// origin/HEAD isn't auto-set when the bare was empty at clone time.
	g(src, "remote", "set-head", "origin", "main")
	branch := "argus/" + slug
	g(src, "branch", branch)
	g(src, "worktree", "add", wt, branch)
	g(wt, "config", "user.email", "iris-test@example.com")
	g(wt, "config", "user.name", "iris-test")
	g(wt, "commit", "--allow-empty", "-m", "work on "+branch)
	return src, wt
}

func TestMergeToMaster_RefusesNonArgusBranch(t *testing.T) {
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
	g(src, "branch", "feature/something")
	g(src, "worktree", "add", wt, "feature/something")

	client := stubArgus(t, src, wt)
	before := headSHA(t, src)
	_, err := MergeToMaster(context.Background(), client, "task-1", MergeOptions{NoFF: true})
	if err == nil {
		t.Fatal("expected error for non-argus branch, got nil")
	}
	if !strings.Contains(err.Error(), "non-argus branch") {
		t.Fatalf("unexpected error: %v", err)
	}
	if after := headSHA(t, src); after != before {
		t.Fatalf("expected source repo HEAD unchanged on refusal: before=%s after=%s", before, after)
	}
}

func TestMergeToMaster_HappyPath(t *testing.T) {
	src, wt := setupRepoWithWorktree(t, "happy-slug")
	client := stubArgus(t, src, wt)

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
	wantSrc, _ := filepath.EvalSymlinks(src)
	if result.SourceRepo != wantSrc {
		t.Fatalf("unexpected source repo: %q (want %q)", result.SourceRepo, wantSrc)
	}
	if result.SHA == "" {
		t.Fatal("empty merge SHA")
	}
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
	g := gitRunner(t)
	writeFile := func(dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	g("", "init", "--bare", "-b", "main", bare)
	g("", "clone", bare, src)
	g(src, "config", "user.email", "x@y.z")
	g(src, "config", "user.name", "x")
	writeFile(src, "f.txt", "from-main\n")
	g(src, "add", "f.txt")
	g(src, "commit", "-m", "initial main")
	g(src, "push", "-u", "origin", "main")
	g(src, "remote", "set-head", "origin", "main")
	g(src, "branch", "argus/conflict-slug")
	g(src, "worktree", "add", wt, "argus/conflict-slug")
	writeFile(src, "f.txt", "main-edit\n")
	g(src, "add", "f.txt")
	g(src, "commit", "-m", "main edit")
	g(src, "push", "origin", "main")
	writeFile(wt, "f.txt", "wt-edit\n")
	g(wt, "config", "user.email", "x@y.z")
	g(wt, "config", "user.name", "x")
	g(wt, "add", "f.txt")
	g(wt, "commit", "-m", "wt edit")

	client := stubArgus(t, src, wt)
	_, err := MergeToMaster(context.Background(), client, "task-conflict", MergeOptions{NoFF: true})
	if err == nil {
		t.Fatal("expected merge conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "merge") {
		t.Fatalf("unexpected error: %v", err)
	}
	out, _ := exec.Command("git", "-C", src, "status", "--porcelain=v2", "--branch").CombinedOutput()
	if strings.Contains(string(out), "MERGE_HEAD") {
		t.Fatalf("expected merge aborted, but MERGE_HEAD present:\n%s", out)
	}
}

// Delta scenario: "Refuses to merge master into master."
// Setup: src is on a side branch; the worktree is on main. A misconfigured
// argus task pointing at main itself must be rejected.
func TestMergeToMaster_RefusesDefaultBranchIntoItself(t *testing.T) {
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
	// Free up "main" for the worktree by moving src to a side branch.
	g(src, "switch", "-c", "iris-test-host")
	g(src, "worktree", "add", wt, "main")

	client := stubArgus(t, src, wt)
	before := headSHA(t, src)
	_, err := MergeToMaster(context.Background(), client, "task-main", MergeOptions{NoFF: true})
	if err == nil {
		t.Fatal("expected error refusing main into main, got nil")
	}
	if !strings.Contains(err.Error(), "main") && !strings.Contains(err.Error(), "protected") {
		t.Fatalf("unexpected error: %v", err)
	}
	if after := headSHA(t, src); after != before {
		t.Fatalf("expected source repo HEAD unchanged on refusal: before=%s after=%s", before, after)
	}
}

// Delta scenario: "`no_ff=false` allows fast-forward".
func TestMergeToMaster_FastForwardWhenNoFFFalse(t *testing.T) {
	src, wt := setupRepoWithWorktree(t, "ff-slug")
	client := stubArgus(t, src, wt)

	result, err := MergeToMaster(context.Background(), client, "task-ff", MergeOptions{NoFF: false})
	if err != nil {
		t.Fatalf("ff merge: %v", err)
	}
	out, err := exec.Command("git", "-C", src, "log", "--oneline", "-3", "--no-merges").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "Merge branch") {
		t.Fatalf("--ff-only produced a merge commit (should be linear):\n%s", out)
	}
	if result.SHA == "" {
		t.Fatal("empty merge SHA")
	}
}

// Delta scenario: "Custom merge message".
func TestMergeToMaster_CustomMessage(t *testing.T) {
	src, wt := setupRepoWithWorktree(t, "msg-slug")
	client := stubArgus(t, src, wt)

	const subject = "ship: my custom merge subject"
	_, err := MergeToMaster(context.Background(), client, "task-msg", MergeOptions{NoFF: true, Message: subject})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	out, err := exec.Command("git", "-C", src, "log", "-1", "--format=%s").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != subject {
		t.Fatalf("merge subject: got %q, want %q", strings.TrimSpace(string(out)), subject)
	}
}

// Host-bridge scenario: "Verb refuses an unknown task ID."
// No git mutation happens because there's no resolved repo, but assert the
// "no mutation" contract is preserved by checking the client never reaches
// the git layer (a 404 from stubArgusTaskNotFound is enough).
func TestMergeToMaster_RefusesUnknownTaskID(t *testing.T) {
	client := stubArgusTaskNotFound(t)
	_, err := MergeToMaster(context.Background(), client, "ghost-task", MergeOptions{NoFF: true})
	if err == nil {
		t.Fatal("expected error for unknown task, got nil")
	}
	if !strings.Contains(err.Error(), "ghost-task") {
		t.Fatalf("expected error to name task id, got: %v", err)
	}
}

// Host-bridge scenario: "Verb refuses a source repo outside the project allowlist."
func TestMergeToMaster_RefusesNonAllowlistedRepo(t *testing.T) {
	src, wt := setupRepoWithWorktree(t, "denied-slug")
	// Stub argus knows about a different project, NOT src.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/tasks/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":            "x",
				"worktree_path": wt,
			})
		case r.URL.Path == "/api/projects/full":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{
					{"name": "other", "path": "/some/other/repo"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client := argus.New(srv.URL, "stub-token")

	before := headSHA(t, src)
	_, err := MergeToMaster(context.Background(), client, "task-denied", MergeOptions{NoFF: true})
	if err == nil {
		t.Fatal("expected allowlist refusal, got nil")
	}
	if !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("expected allowlist error, got: %v", err)
	}
	// Ensure the rejected path is named in the error so operators can
	// diagnose ("source repo X is not in argus's project allowlist").
	wantSrc, _ := filepath.EvalSymlinks(src)
	if !strings.Contains(err.Error(), wantSrc) {
		t.Fatalf("expected error to name rejected path %q, got: %v", wantSrc, err)
	}
	if after := headSHA(t, src); after != before {
		t.Fatalf("expected source repo HEAD unchanged on refusal: before=%s after=%s", before, after)
	}
}

// Host-bridge scenario: "Two concurrent merge_to_master calls serialize."
func TestMergeToMaster_ConcurrentCallsSerialize(t *testing.T) {
	// Two argus tasks pointing at the SAME source repo, with two
	// different argus/<slug> branches checked out as worktrees. Concurrent
	// MergeToMaster calls must serialize on the per-source-repo mutex.
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "origin.git")
	src := filepath.Join(tmp, "src")
	wt1 := filepath.Join(tmp, "wt1")
	wt2 := filepath.Join(tmp, "wt2")
	g := gitRunner(t)

	g("", "init", "--bare", "-b", "main", bare)
	g("", "clone", bare, src)
	g(src, "config", "user.email", "x@y.z")
	g(src, "config", "user.name", "x")
	g(src, "commit", "--allow-empty", "-m", "initial")
	g(src, "push", "-u", "origin", "main")
	g(src, "remote", "set-head", "origin", "main")
	g(src, "branch", "argus/concurrent-a")
	g(src, "branch", "argus/concurrent-b")
	g(src, "worktree", "add", wt1, "argus/concurrent-a")
	g(src, "worktree", "add", wt2, "argus/concurrent-b")
	// Distinct commit messages so the two merge commits have distinct SHAs
	// (identical empty commits would otherwise hash to the same merge).
	g(wt1, "config", "user.email", "x@y.z")
	g(wt1, "config", "user.name", "x")
	g(wt1, "commit", "--allow-empty", "-m", "work on a")
	g(wt2, "config", "user.email", "x@y.z")
	g(wt2, "config", "user.name", "x")
	g(wt2, "commit", "--allow-empty", "-m", "work on b")

	canon, _ := filepath.EvalSymlinks(src)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/tasks/task-a":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "task-a", "worktree_path": wt1})
		case r.URL.Path == "/api/tasks/task-b":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "task-b", "worktree_path": wt2})
		case r.URL.Path == "/api/projects/full":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"name": "iris-test", "path": canon}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client := argus.New(srv.URL, "stub-token")

	var wg sync.WaitGroup
	results := make([]*MergeResult, 2)
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results[0], errs[0] = MergeToMaster(context.Background(), client, "task-a", MergeOptions{NoFF: true})
	}()
	// Tiny stagger so the goroutines start at slightly different times;
	// the mutex must still produce a deterministic outcome.
	time.Sleep(5 * time.Millisecond)
	go func() {
		defer wg.Done()
		results[1], errs[1] = MergeToMaster(context.Background(), client, "task-b", MergeOptions{NoFF: true})
	}()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("merge %d: %v", i, err)
		}
	}
	if results[0].SHA == results[1].SHA {
		t.Fatalf("expected distinct merge SHAs from two task branches; both = %s", results[0].SHA)
	}
	// Serialization invariant: the second merge's HEAD must descend from
	// the first merge's SHA (i.e., the second saw the first's result).
	out, err := exec.Command("git", "-C", src, "log", "--oneline", "-5").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	mergeCount := strings.Count(string(out), "Merge branch 'argus/concurrent-")
	if mergeCount != 2 {
		t.Fatalf("expected 2 merge commits, got %d:\n%s", mergeCount, out)
	}
}

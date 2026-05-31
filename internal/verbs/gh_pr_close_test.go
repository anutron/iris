package verbs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anutron/iris/internal/argus"
)

func TestGHPRClose_HappyPathNoDeleteBranch(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghclose-noflag")
	client := stubArgus(t, src, wt)
	body := fakeGHCaptureArgv + `
echo "✓ Closed pull request #7"
exit 0
`
	dir := writeFakeGH(t, body)

	result, err := GHPRClose(context.Background(), client, "task-close", GHPRCloseOptions{PRNumber: 7})
	if err != nil {
		t.Fatalf("gh pr close: %v", err)
	}
	if !result.Closed {
		t.Fatal("expected Closed=true")
	}
	if result.BranchDeleted {
		t.Fatal("expected BranchDeleted=false when delete_branch not set")
	}
	argv := readFakeGHArgv(t, dir)
	if !strings.Contains(argv, "pr") || !strings.Contains(argv, "close") {
		t.Fatalf("argv missing 'pr close':\n%s", argv)
	}
	if !strings.Contains(argv, "7") {
		t.Fatalf("argv missing PR number 7:\n%s", argv)
	}
	if strings.Contains(argv, "--delete-branch") {
		t.Fatalf("expected --delete-branch NOT to be present; argv:\n%s", argv)
	}
}

func TestGHPRClose_HappyPathWithDeleteBranch(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghclose-delete")
	client := stubArgus(t, src, wt)
	body := fakeGHCaptureArgv + `
echo "✓ Closed pull request #8"
echo "✓ Deleted branch argus/whatever"
exit 0
`
	dir := writeFakeGH(t, body)

	result, err := GHPRClose(context.Background(), client, "task-close-d", GHPRCloseOptions{
		PRNumber:     8,
		DeleteBranch: true,
	})
	if err != nil {
		t.Fatalf("gh pr close: %v", err)
	}
	if !result.Closed {
		t.Fatal("expected Closed=true")
	}
	if !result.BranchDeleted {
		t.Fatal("expected BranchDeleted=true when delete_branch=true")
	}
	argv := readFakeGHArgv(t, dir)
	if !strings.Contains(argv, "--delete-branch") {
		t.Fatalf("argv missing --delete-branch when delete_branch=true:\n%s", argv)
	}
}

func TestGHPRClose_GHNonZeroReturnsStructuredError(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghclose-err")
	client := stubArgus(t, src, wt)
	body := fakeGHCaptureArgv + `
echo "could not close PR #99: not found" 1>&2
exit 1
`
	writeFakeGH(t, body)

	_, err := GHPRClose(context.Background(), client, "task-cerr", GHPRCloseOptions{PRNumber: 99})
	if err == nil {
		t.Fatal("expected error from non-zero gh, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected gh stderr in error, got: %v", err)
	}
}

func TestGHPRClose_AlreadyClosedSurfacedAsError(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghclose-already")
	client := stubArgus(t, src, wt)
	body := fakeGHCaptureArgv + `
echo "Pull request #5 is already closed" 1>&2
exit 1
`
	writeFakeGH(t, body)

	_, err := GHPRClose(context.Background(), client, "task-already", GHPRCloseOptions{PRNumber: 5})
	if err == nil {
		t.Fatal("expected error for already-closed PR, got nil")
	}
	if !strings.Contains(err.Error(), "already closed") {
		t.Fatalf("expected gh stderr verbatim in error, got: %v", err)
	}
}

func TestGHPRClose_RefusesUnknownTaskID(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t)
	_, err := GHPRClose(context.Background(), client, "ghost-task", GHPRCloseOptions{PRNumber: 1})
	if err == nil {
		t.Fatal("expected error for unknown task, got nil")
	}
	if !strings.Contains(err.Error(), "ghost-task") {
		t.Fatalf("expected error to name task id, got: %v", err)
	}
}

func TestGHPRClose_RefusesNonAllowlistedRepo(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghclose-denied")
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

	_, err := GHPRClose(context.Background(), client, "task-denied", GHPRCloseOptions{PRNumber: 1})
	if err == nil {
		t.Fatal("expected allowlist refusal, got nil")
	}
	if !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("expected allowlist error, got: %v", err)
	}
	wantSrc, _ := filepath.EvalSymlinks(src)
	if !strings.Contains(err.Error(), wantSrc) {
		t.Fatalf("expected error to name rejected path %q, got: %v", wantSrc, err)
	}
}

func TestGHPRClose_RejectsNonPositivePR(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t)
	_, err := GHPRClose(context.Background(), client, "task-x", GHPRCloseOptions{PRNumber: 0})
	if err == nil {
		t.Fatal("expected error for pr_number=0, got nil")
	}
	if !strings.Contains(err.Error(), "pr_number") {
		t.Fatalf("expected pr_number validation error, got: %v", err)
	}
}

func TestGHPRClose_ConcurrentCallsSerialize(t *testing.T) {
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
	g(src, "branch", "argus/close-a")
	g(src, "branch", "argus/close-b")
	g(src, "worktree", "add", wt1, "argus/close-a")
	g(src, "worktree", "add", wt2, "argus/close-b")

	canon, _ := filepath.EvalSymlinks(src)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tasks/task-a":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "task-a", "worktree_path": wt1})
		case "/api/tasks/task-b":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "task-b", "worktree_path": wt2})
		case "/api/projects/full":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"name": "iris-test", "path": canon}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client := argus.New(srv.URL, "stub-token")

	body := `
sleep 0.15
echo "closed"
exit 0
`
	writeFakeGH(t, body)

	start := time.Now()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = GHPRClose(context.Background(), client, "task-a", GHPRCloseOptions{PRNumber: 1})
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = GHPRClose(context.Background(), client, "task-b", GHPRCloseOptions{PRNumber: 2})
	}()
	wg.Wait()
	elapsed := time.Since(start)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if elapsed < 250*time.Millisecond {
		t.Fatalf("expected serialized calls (≥250ms total); got %v", elapsed)
	}
}

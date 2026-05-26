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

func TestBranchDeleteRemote_HappyPath(t *testing.T) {
	src, wt, bare := setupRepoWithBareAndWorktree(t, "bdr-happy")
	client := stubArgus(t, src, wt)

	// Push the argus/<slug> branch to origin so we have something to delete.
	g := gitRunner(t)
	g(src, "push", "origin", "argus/bdr-happy")

	priorSHA := remoteRef(t, bare, "refs/heads/argus/bdr-happy")
	if priorSHA == "" {
		t.Fatal("branch was not on origin before delete")
	}

	result, err := BranchDeleteRemote(context.Background(), BranchDeleteRemoteInput{
		Client: client, TaskID: "task-bdr", Branch: "argus/bdr-happy",
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !result.Deleted {
		t.Fatal("expected Deleted=true")
	}
	if result.Branch != "argus/bdr-happy" {
		t.Fatalf("unexpected branch: %q", result.Branch)
	}
	if result.PriorRemoteSHA != priorSHA {
		t.Fatalf("PriorRemoteSHA mismatch: got %q, want %q", result.PriorRemoteSHA, priorSHA)
	}
	if remoteRef(t, bare, "refs/heads/argus/bdr-happy") != "" {
		t.Fatal("branch still present on origin after delete")
	}
}

func TestBranchDeleteRemote_RefusesDefaultBranch(t *testing.T) {
	src, wt, bare := setupRepoWithBareAndWorktree(t, "bdr-default")
	client := stubArgus(t, src, wt)

	before := remoteRef(t, bare, "refs/heads/main")
	_, err := BranchDeleteRemote(context.Background(), BranchDeleteRemoteInput{
		Client: client, TaskID: "task-default", Branch: "main",
	})
	if err == nil {
		t.Fatal("expected error refusing default branch, got nil")
	}
	if !strings.Contains(err.Error(), "main") || !strings.Contains(err.Error(), "default") {
		t.Fatalf("expected error to name both branch and default, got: %v", err)
	}
	if after := remoteRef(t, bare, "refs/heads/main"); after != before {
		t.Fatalf("expected origin/main untouched: before=%s after=%s", before, after)
	}
}

func TestBranchDeleteRemote_RefusesEmptyBranch(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "bdr-empty")
	client := stubArgus(t, src, wt)

	_, err := BranchDeleteRemote(context.Background(), BranchDeleteRemoteInput{
		Client: client, TaskID: "task-empty", Branch: "",
	})
	if err == nil {
		t.Fatal("expected error for empty branch, got nil")
	}
	if !strings.Contains(err.Error(), "branch") {
		t.Fatalf("expected error to mention branch, got: %v", err)
	}
}

func TestBranchDeleteRemote_RefusesNonExistentBranch(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "bdr-missing")
	client := stubArgus(t, src, wt)

	_, err := BranchDeleteRemote(context.Background(), BranchDeleteRemoteInput{
		Client: client, TaskID: "task-missing", Branch: "argus/never-pushed",
	})
	if err == nil {
		t.Fatal("expected error for missing branch, got nil")
	}
	if !strings.Contains(err.Error(), "argus/never-pushed") {
		t.Fatalf("expected error to name the missing branch, got: %v", err)
	}
}

func TestBranchDeleteRemote_RefusesUnknownTaskID(t *testing.T) {
	client := stubArgusTaskNotFound(t)
	_, err := BranchDeleteRemote(context.Background(), BranchDeleteRemoteInput{
		Client: client, TaskID: "ghost-task", Branch: "argus/something",
	})
	if err == nil {
		t.Fatal("expected error for unknown task, got nil")
	}
	if !strings.Contains(err.Error(), "ghost-task") {
		t.Fatalf("expected error to name task id, got: %v", err)
	}
}

func TestBranchDeleteRemote_RefusesNonAllowlistedRepo(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "bdr-denied")
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

	_, err := BranchDeleteRemote(context.Background(), BranchDeleteRemoteInput{
		Client: client, TaskID: "task-denied", Branch: "argus/bdr-denied",
	})
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

// Concurrency invariant: two BranchDeleteRemote calls against the same
// source repo serialize on repoLocks. Each call targets a distinct
// branch so both can succeed; the test passes only when -race sees no
// concurrent git invocations stomping on the index lock.
func TestBranchDeleteRemote_LockSerializesCalls(t *testing.T) {
	src, wt, bare := setupRepoWithBareAndWorktree(t, "bdr-lock-a")
	g := gitRunner(t)
	g(src, "branch", "argus/bdr-lock-b")
	g(src, "push", "origin", "argus/bdr-lock-a")
	g(src, "push", "origin", "argus/bdr-lock-b")

	canon, _ := filepath.EvalSymlinks(src)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tasks/task-a":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "task-a", "worktree_path": wt})
		case "/api/tasks/task-b":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "task-b", "worktree_path": wt})
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

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = BranchDeleteRemote(context.Background(), BranchDeleteRemoteInput{
			Client: client, TaskID: "task-a", Branch: "argus/bdr-lock-a",
		})
	}()
	time.Sleep(5 * time.Millisecond)
	go func() {
		defer wg.Done()
		_, errs[1] = BranchDeleteRemote(context.Background(), BranchDeleteRemoteInput{
			Client: client, TaskID: "task-b", Branch: "argus/bdr-lock-b",
		})
	}()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent delete %d: %v", i, err)
		}
	}
	if remoteRef(t, bare, "refs/heads/argus/bdr-lock-a") != "" {
		t.Fatal("branch a still present after delete")
	}
	if remoteRef(t, bare, "refs/heads/argus/bdr-lock-b") != "" {
		t.Fatal("branch b still present after delete")
	}
}

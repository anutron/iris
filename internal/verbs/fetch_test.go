package verbs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anutron/iris/internal/argus"
)

// updateOriginBranch pushes a competing commit on `branch` to the bare
// origin via a sidecar clone, returning origin/<branch>'s new tip SHA.
// Used by fetch tests to manufacture an "origin moved" scenario.
func updateOriginBranch(t *testing.T, bare, branch string) string {
	t.Helper()
	tmp := t.TempDir()
	side := filepath.Join(tmp, "side")
	g := gitRunner(t)
	g("", "clone", bare, side)
	g(side, "config", "user.email", "x@y.z")
	g(side, "config", "user.name", "x")
	// Try local checkout; otherwise create from origin/<branch>.
	if _, err := exec.Command("git", "-C", side, "rev-parse", "--verify", "refs/heads/"+branch).CombinedOutput(); err == nil {
		g(side, "switch", branch)
	} else {
		g(side, "fetch", "origin")
		g(side, "switch", "-c", branch, "origin/"+branch)
	}
	g(side, "commit", "--allow-empty", "-m", "competing on "+branch)
	g(side, "push", "origin", branch)
	out, err := exec.Command("git", "-C", side, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD on sidecar: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestFetch_HappyPathReturnsUpdatedRefs(t *testing.T) {
	t.Parallel()
	src, wt, bare := setupRepoWithBareAndWorktree(t, "fetch-happy")
	client := stubArgus(t, src, wt)

	mainBefore := remoteRef(t, src, "refs/remotes/origin/main")
	newMain := updateOriginBranch(t, bare, "main")
	if newMain == mainBefore {
		t.Fatal("sidecar failed to update origin/main")
	}

	result, err := Fetch(context.Background(), FetchInput{Client: client, TaskID: "task-fetch"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !result.Fetched {
		t.Fatal("expected Fetched=true")
	}
	found := false
	for _, u := range result.RefsUpdated {
		if u.Ref == "refs/remotes/origin/main" {
			if u.OldSHA != mainBefore {
				t.Fatalf("OldSHA mismatch: got %q, want %q", u.OldSHA, mainBefore)
			}
			if u.NewSHA != newMain {
				t.Fatalf("NewSHA mismatch: got %q, want %q", u.NewSHA, newMain)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("expected refs/remotes/origin/main in result, got %#v", result.RefsUpdated)
	}
}

func TestFetch_UpToDateReturnsEmptyUpdates(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "fetch-uptodate")
	client := stubArgus(t, src, wt)

	result, err := Fetch(context.Background(), FetchInput{Client: client, TaskID: "task-uptodate"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !result.Fetched {
		t.Fatal("expected Fetched=true")
	}
	if len(result.RefsUpdated) != 0 {
		t.Fatalf("expected empty refs_updated, got %#v", result.RefsUpdated)
	}
}

func TestFetch_NonZeroGitExitReturnsError(t *testing.T) {
	t.Parallel()
	src, wt, bare := setupRepoWithBareAndWorktree(t, "fetch-broken")
	client := stubArgus(t, src, wt)

	g := gitRunner(t)
	g(src, "remote", "set-url", "origin", bare+"-does-not-exist")

	_, err := Fetch(context.Background(), FetchInput{Client: client, TaskID: "task-broken"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "fetch") {
		t.Fatalf("expected error mentioning fetch, got: %v", err)
	}
}

func TestFetch_RefusesUnknownTaskID(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t)
	_, err := Fetch(context.Background(), FetchInput{Client: client, TaskID: "ghost-task"})
	if err == nil {
		t.Fatal("expected error for unknown task, got nil")
	}
	if !strings.Contains(err.Error(), "ghost-task") {
		t.Fatalf("expected error to name task id, got: %v", err)
	}
}

func TestFetch_RefusesNonAllowlistedRepo(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "fetch-denied")
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

	_, err := Fetch(context.Background(), FetchInput{Client: client, TaskID: "task-denied"})
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

func TestFetch_PerSourceRepoLockHeld(t *testing.T) {
	t.Parallel()
	src, wt, bare := setupRepoWithBareAndWorktree(t, "fetch-lock")
	client := stubArgus(t, src, wt)

	// Two concurrent fetches on the same source repo must serialize on
	// repoLocks. With repoLocks holding both calls won't crash; without
	// it, race-detector runs surface concurrent git invocations stomping
	// on the repo's index lock.
	updateOriginBranch(t, bare, "main")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = Fetch(context.Background(), FetchInput{Client: client, TaskID: "task-a"})
	}()
	time.Sleep(5 * time.Millisecond)
	go func() {
		defer wg.Done()
		_, errs[1] = Fetch(context.Background(), FetchInput{Client: client, TaskID: "task-b"})
	}()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent fetch %d: %v", i, err)
		}
	}
	_ = src
}

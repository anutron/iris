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

func TestTag_HappyPathCreatesAndPushes(t *testing.T) {
	src, wt, bare := setupRepoWithBareAndWorktree(t, "tag-happy")
	client := stubArgus(t, src, wt)

	originMainSHA := remoteRef(t, src, "refs/remotes/origin/main")
	if originMainSHA == "" {
		t.Fatal("origin/main not set on src")
	}

	result, err := Tag(context.Background(), TagInput{
		Client: client, TaskID: "task-tag", Tag: "v1.0.0", Message: "release v1.0.0",
	})
	if err != nil {
		t.Fatalf("tag: %v", err)
	}
	if !result.Tagged {
		t.Fatal("expected Tagged=true")
	}
	if result.Tag != "v1.0.0" {
		t.Fatalf("unexpected tag: %q", result.Tag)
	}
	if result.SHA != originMainSHA {
		t.Fatalf("SHA mismatch: got %q, want %q", result.SHA, originMainSHA)
	}
	if result.Message != "release v1.0.0" {
		t.Fatalf("unexpected message: %q", result.Message)
	}

	// Tag must be on origin.
	remoteTagSHA := remoteRef(t, bare, "refs/tags/v1.0.0^{}")
	if remoteTagSHA == "" {
		// Annotated tag: peeling may differ; also check the plain ref.
		remoteTagSHA = remoteRef(t, bare, "refs/tags/v1.0.0")
	}
	if remoteTagSHA == "" {
		t.Fatal("tag not on origin after push")
	}

	// Annotated tag: `git cat-file -t <tag>` should be "tag".
	out, err := exec.Command("git", "-C", src, "cat-file", "-t", "v1.0.0").Output()
	if err != nil {
		t.Fatalf("cat-file: %v", err)
	}
	if strings.TrimSpace(string(out)) != "tag" {
		t.Fatalf("expected annotated tag, got %q", strings.TrimSpace(string(out)))
	}
}

func TestTag_DefaultMessageWhenEmpty(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "tag-default-msg")
	client := stubArgus(t, src, wt)

	result, err := Tag(context.Background(), TagInput{
		Client: client, TaskID: "task-tag-msg", Tag: "v0.0.1", Message: "",
	})
	if err != nil {
		t.Fatalf("tag: %v", err)
	}
	if result.Message != "Released by iris" {
		t.Fatalf("expected default message, got %q", result.Message)
	}
	out, err := exec.Command("git", "-C", src, "tag", "-l", "v0.0.1", "-n1").Output()
	if err != nil {
		t.Fatalf("tag -l: %v", err)
	}
	if !strings.Contains(string(out), "Released by iris") {
		t.Fatalf("expected annotation 'Released by iris' in tag output, got: %s", out)
	}
}

func TestTag_RefusesExistingLocalTag(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "tag-local-exists")
	client := stubArgus(t, src, wt)

	g := gitRunner(t)
	g(src, "tag", "-a", "v1.2.3", "-m", "pre-existing")

	_, err := Tag(context.Background(), TagInput{
		Client: client, TaskID: "task-tag-local", Tag: "v1.2.3", Message: "should refuse",
	})
	if err == nil {
		t.Fatal("expected error for existing local tag, got nil")
	}
	if !strings.Contains(err.Error(), "v1.2.3") {
		t.Fatalf("expected error to name the conflict, got: %v", err)
	}
}

func TestTag_RefusesExistingRemoteTag(t *testing.T) {
	src, wt, bare := setupRepoWithBareAndWorktree(t, "tag-remote-exists")
	client := stubArgus(t, src, wt)

	// Create a tag on a sidecar clone and push to origin so iris's source
	// repo has no local tag, only the remote does.
	tmp := t.TempDir()
	side := filepath.Join(tmp, "side")
	g := gitRunner(t)
	g("", "clone", bare, side)
	g(side, "config", "user.email", "x@y.z")
	g(side, "config", "user.name", "x")
	g(side, "tag", "-a", "v9.9.9", "-m", "remote pre-existing")
	g(side, "push", "origin", "v9.9.9")

	// Sanity: source repo does NOT have the tag locally.
	if _, err := exec.Command("git", "-C", src, "rev-parse", "v9.9.9").CombinedOutput(); err == nil {
		t.Fatal("test setup: source repo should not have the tag locally yet")
	}

	_, err := Tag(context.Background(), TagInput{
		Client: client, TaskID: "task-tag-remote", Tag: "v9.9.9", Message: "should refuse",
	})
	if err == nil {
		t.Fatal("expected error for existing remote tag, got nil")
	}
	if !strings.Contains(err.Error(), "v9.9.9") {
		t.Fatalf("expected error to name the remote tag, got: %v", err)
	}
}

func TestTag_RefusesArgvFlagSmuggling(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "tag-flagsmuggle")
	client := stubArgus(t, src, wt)

	_, err := Tag(context.Background(), TagInput{
		Client: client, TaskID: "task-tag-flag", Tag: "--exec=evil", Message: "x",
	})
	if err == nil {
		t.Fatal("expected error refusing leading-dash tag, got nil")
	}
	if !strings.Contains(err.Error(), "invalid tag") {
		t.Fatalf("expected 'invalid tag' error, got: %v", err)
	}
}

func TestTag_RefusesEmptyTag(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "tag-empty")
	client := stubArgus(t, src, wt)

	_, err := Tag(context.Background(), TagInput{
		Client: client, TaskID: "task-tag-empty", Tag: "", Message: "x",
	})
	if err == nil {
		t.Fatal("expected error for empty tag, got nil")
	}
	if !strings.Contains(err.Error(), "tag") {
		t.Fatalf("expected error to mention tag, got: %v", err)
	}
}

func TestTag_NonZeroGitExitReturnsError(t *testing.T) {
	src, wt, bare := setupRepoWithBareAndWorktree(t, "tag-broken")
	client := stubArgus(t, src, wt)

	// Break the remote URL so the push step fails. The local tag-create
	// will still succeed; we just want a non-zero exit on push surfaced.
	g := gitRunner(t)
	g(src, "remote", "set-url", "origin", bare+"-does-not-exist")

	_, err := Tag(context.Background(), TagInput{
		Client: client, TaskID: "task-tag-broken", Tag: "v0.0.99", Message: "should fail push",
	})
	if err == nil {
		t.Fatal("expected error from broken push, got nil")
	}
}

func TestTag_RefusesUnknownTaskID(t *testing.T) {
	client := stubArgusTaskNotFound(t)
	_, err := Tag(context.Background(), TagInput{
		Client: client, TaskID: "ghost-task", Tag: "v1.0.0",
	})
	if err == nil {
		t.Fatal("expected error for unknown task, got nil")
	}
	if !strings.Contains(err.Error(), "ghost-task") {
		t.Fatalf("expected error to name task id, got: %v", err)
	}
}

func TestTag_RefusesNonAllowlistedRepo(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "tag-denied")
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

	_, err := Tag(context.Background(), TagInput{
		Client: client, TaskID: "task-denied", Tag: "v1.0.0",
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

func TestTag_LockSerializesCalls(t *testing.T) {
	src, wt, bare := setupRepoWithBareAndWorktree(t, "tag-lock")
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
		_, errs[0] = Tag(context.Background(), TagInput{
			Client: client, TaskID: "task-a", Tag: "v0.0.1", Message: "a",
		})
	}()
	time.Sleep(5 * time.Millisecond)
	go func() {
		defer wg.Done()
		_, errs[1] = Tag(context.Background(), TagInput{
			Client: client, TaskID: "task-b", Tag: "v0.0.2", Message: "b",
		})
	}()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent tag %d: %v", i, err)
		}
	}
	if remoteRef(t, bare, "refs/tags/v0.0.1") == "" {
		t.Fatal("tag a missing on origin")
	}
	if remoteRef(t, bare, "refs/tags/v0.0.2") == "" {
		t.Fatal("tag b missing on origin")
	}
}

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

func TestGHPRComment_HappyPathReturnsURL(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghcomment-happy")
	client := stubArgus(t, src, wt)
	body := fakeGHCaptureArgv + `
echo "https://github.com/anutron/iris/pull/42#issuecomment-1234567890"
exit 0
`
	dir := writeFakeGH(t, body)

	result, err := GHPRComment(context.Background(), client, "task-comment", GHPRCommentOptions{
		PRNumber: 42,
		Body:     "hello from iris",
	})
	if err != nil {
		t.Fatalf("gh pr comment: %v", err)
	}
	if result.URL != "https://github.com/anutron/iris/pull/42#issuecomment-1234567890" {
		t.Fatalf("URL mismatch: %q", result.URL)
	}
	if result.ParseWarning != "" {
		t.Fatalf("unexpected parse_warning on happy path: %q", result.ParseWarning)
	}
	argv := readFakeGHArgv(t, dir)
	if !strings.Contains(argv, "pr") || !strings.Contains(argv, "comment") {
		t.Fatalf("argv missing pr comment:\n%s", argv)
	}
	if !strings.Contains(argv, "42") {
		t.Fatalf("argv missing PR number 42:\n%s", argv)
	}
	if !strings.Contains(argv, "--body") {
		t.Fatalf("argv missing --body:\n%s", argv)
	}
	if !strings.Contains(argv, "hello from iris") {
		t.Fatalf("argv missing body value:\n%s", argv)
	}
}

func TestGHPRComment_EmptyBodyRefusedBeforeGH(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghcomment-empty")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHCaptureArgv+"\nexit 0\n")

	_, err := GHPRComment(context.Background(), client, "task-empty", GHPRCommentOptions{PRNumber: 1, Body: ""})
	if err == nil {
		t.Fatal("expected error for empty body, got nil")
	}
	if !strings.Contains(err.Error(), "body") {
		t.Fatalf("expected body validation error, got: %v", err)
	}
	if argv := readFakeGHArgv(t, dir); argv != "" {
		t.Fatalf("expected gh NOT to be invoked, but argv was captured:\n%s", argv)
	}
}

func TestGHPRComment_WhitespaceOnlyBodyRefused(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghcomment-ws")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHCaptureArgv+"\nexit 0\n")

	_, err := GHPRComment(context.Background(), client, "task-ws", GHPRCommentOptions{PRNumber: 1, Body: "   \n  "})
	if err == nil {
		t.Fatal("expected error for whitespace-only body, got nil")
	}
	if argv := readFakeGHArgv(t, dir); argv != "" {
		t.Fatalf("expected gh NOT to be invoked, argv was captured:\n%s", argv)
	}
}

func TestGHPRComment_ParseFailureSurfacesWarning(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghcomment-parse")
	client := stubArgus(t, src, wt)
	// gh exits 0 but stdout has no URL.
	body := fakeGHCaptureArgv + `
echo "Comment posted successfully"
exit 0
`
	writeFakeGH(t, body)

	result, err := GHPRComment(context.Background(), client, "task-parse", GHPRCommentOptions{PRNumber: 1, Body: "x"})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if result.URL != "" {
		t.Fatalf("expected empty URL on parse failure, got: %q", result.URL)
	}
	if result.ParseWarning == "" {
		t.Fatal("expected parse_warning to be non-empty on parse failure")
	}
	if !strings.Contains(result.ParseWarning, "Comment posted successfully") {
		t.Fatalf("expected raw stdout in parse_warning, got: %q", result.ParseWarning)
	}
}

func TestGHPRComment_GHNonZeroReturnsStructuredError(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghcomment-err")
	client := stubArgus(t, src, wt)
	body := fakeGHCaptureArgv + `
echo "error: not found" 1>&2
exit 1
`
	writeFakeGH(t, body)

	_, err := GHPRComment(context.Background(), client, "task-err", GHPRCommentOptions{PRNumber: 1, Body: "x"})
	if err == nil {
		t.Fatal("expected error from non-zero gh, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected gh stderr in error, got: %v", err)
	}
}

func TestGHPRComment_RefusesUnknownTaskID(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t)
	_, err := GHPRComment(context.Background(), client, "ghost-task", GHPRCommentOptions{PRNumber: 1, Body: "x"})
	if err == nil {
		t.Fatal("expected error for unknown task, got nil")
	}
	if !strings.Contains(err.Error(), "ghost-task") {
		t.Fatalf("expected error to name task id, got: %v", err)
	}
}

func TestGHPRComment_RefusesNonAllowlistedRepo(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghcomment-denied")
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

	_, err := GHPRComment(context.Background(), client, "task-denied", GHPRCommentOptions{PRNumber: 1, Body: "x"})
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

func TestGHPRComment_RejectsNonPositivePR(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t)
	_, err := GHPRComment(context.Background(), client, "task-x", GHPRCommentOptions{PRNumber: 0, Body: "x"})
	if err == nil {
		t.Fatal("expected error for pr_number=0, got nil")
	}
	if !strings.Contains(err.Error(), "pr_number") {
		t.Fatalf("expected pr_number validation error, got: %v", err)
	}
}

func TestGHPRComment_ConcurrentCallsSerialize(t *testing.T) {
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
	g(src, "branch", "argus/comment-a")
	g(src, "branch", "argus/comment-b")
	g(src, "worktree", "add", wt1, "argus/comment-a")
	g(src, "worktree", "add", wt2, "argus/comment-b")

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
echo "https://github.com/x/y/pull/1#issuecomment-1"
exit 0
`
	writeFakeGH(t, body)

	start := time.Now()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = GHPRComment(context.Background(), client, "task-a", GHPRCommentOptions{PRNumber: 1, Body: "x"})
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = GHPRComment(context.Background(), client, "task-b", GHPRCommentOptions{PRNumber: 2, Body: "y"})
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

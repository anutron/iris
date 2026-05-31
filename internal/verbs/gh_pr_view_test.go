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

func TestGHPRView_HappyPathReturnsParsedJSON(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghview-happy")
	client := stubArgus(t, src, wt)

	body := fakeGHCaptureArgv + `
cat <<EOF
{"state":"OPEN","isDraft":false,"headRefName":"argus/ghview-happy","baseRefName":"main","mergeable":"MERGEABLE","checks":[],"reviews":[],"statusCheckRollup":[]}
EOF
exit 0
`
	dir := writeFakeGH(t, body)

	result, err := GHPRView(context.Background(), client, "task-view", GHPRViewOptions{PRNumber: 42})
	if err != nil {
		t.Fatalf("gh pr view: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if got := result.Data["state"]; got != "OPEN" {
		t.Fatalf("state mismatch: got %v, want OPEN", got)
	}
	if got := result.Data["headRefName"]; got != "argus/ghview-happy" {
		t.Fatalf("headRefName mismatch: got %v", got)
	}

	argv := readFakeGHArgv(t, dir)
	if argv == "" {
		t.Fatal("expected gh to be invoked")
	}
	if !strings.Contains(argv, "pr") || !strings.Contains(argv, "view") {
		t.Fatalf("argv missing 'pr view':\n%s", argv)
	}
	if !strings.Contains(argv, "42") {
		t.Fatalf("argv missing PR number 42:\n%s", argv)
	}
	if !strings.Contains(argv, "--json") {
		t.Fatalf("argv missing --json flag:\n%s", argv)
	}
	for _, field := range []string{"state", "checks", "reviews", "mergeable", "headRefName", "baseRefName", "isDraft", "statusCheckRollup"} {
		if !strings.Contains(argv, field) {
			t.Fatalf("argv missing --json field %q:\n%s", field, argv)
		}
	}
}

func TestGHPRView_GHNonZeroReturnsStructuredError(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghview-err")
	client := stubArgus(t, src, wt)
	body := fakeGHCaptureArgv + `
echo "stdout from gh"
echo "GraphQL: Could not resolve to a PullRequest with the number of 999." 1>&2
exit 1
`
	writeFakeGH(t, body)

	_, err := GHPRView(context.Background(), client, "task-view-err", GHPRViewOptions{PRNumber: 999})
	if err == nil {
		t.Fatal("expected error from non-zero gh exit, got nil")
	}
	if !strings.Contains(err.Error(), "Could not resolve") {
		t.Fatalf("expected gh stderr in error, got: %v", err)
	}
}

// TestGHPRView_MalformedJSONReturnsStructuredError covers the case where
// gh exits 0 but emits non-JSON stdout (e.g., a future gh version changes
// output shape or a CDN injects HTML). The contract requires a structured
// error rather than a silent nil-map pass-through.
func TestGHPRView_MalformedJSONReturnsStructuredError(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghview-malformed")
	client := stubArgus(t, src, wt)
	body := fakeGHCaptureArgv + `
echo "this is not JSON"
exit 0
`
	writeFakeGH(t, body)

	_, err := GHPRView(context.Background(), client, "task-malformed-json", GHPRViewOptions{PRNumber: 1})
	if err == nil {
		t.Fatal("expected error from malformed JSON stdout, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "json") &&
		!strings.Contains(strings.ToLower(err.Error()), "parse") &&
		!strings.Contains(strings.ToLower(err.Error()), "invalid") {
		t.Fatalf("expected JSON/parse error indicator, got: %v", err)
	}
}

func TestGHPRView_RefusesUnknownTaskID(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t)
	_, err := GHPRView(context.Background(), client, "ghost-task", GHPRViewOptions{PRNumber: 1})
	if err == nil {
		t.Fatal("expected error for unknown task, got nil")
	}
	if !strings.Contains(err.Error(), "ghost-task") {
		t.Fatalf("expected error to name task id, got: %v", err)
	}
}

func TestGHPRView_RefusesNonAllowlistedRepo(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghview-denied")
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

	_, err := GHPRView(context.Background(), client, "task-denied", GHPRViewOptions{PRNumber: 1})
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

func TestGHPRView_RejectsNonPositivePR(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t) // never reached
	_, err := GHPRView(context.Background(), client, "task-x", GHPRViewOptions{PRNumber: 0})
	if err == nil {
		t.Fatal("expected error for pr_number=0, got nil")
	}
	if !strings.Contains(err.Error(), "pr_number") {
		t.Fatalf("expected pr_number validation error, got: %v", err)
	}
}

// Per-source-repo lock invariant: two concurrent GHPRView calls against the
// same source repo serialize. The fake gh records a sequence number into a
// shared file; if the lock is held, the second call enters only after the
// first exits, so the recorded ordering is strictly serial.
func TestGHPRView_ConcurrentCallsSerialize(t *testing.T) {
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
	g(src, "branch", "argus/view-a")
	g(src, "branch", "argus/view-b")
	g(src, "worktree", "add", wt1, "argus/view-a")
	g(src, "worktree", "add", wt2, "argus/view-b")

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

	// Fake gh: sleep 150ms then emit a minimal JSON. If both calls ran in
	// parallel, total wall time ≈ 150ms; serialized, ≈ 300ms.
	body := `
sleep 0.15
echo '{"state":"OPEN"}'
exit 0
`
	writeFakeGH(t, body)

	start := time.Now()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = GHPRView(context.Background(), client, "task-a", GHPRViewOptions{PRNumber: 1})
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = GHPRView(context.Background(), client, "task-b", GHPRViewOptions{PRNumber: 2})
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

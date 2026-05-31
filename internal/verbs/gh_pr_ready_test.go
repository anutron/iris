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

// fakeGHDispatch dispatches a fake gh script based on argv. The script
// inspects $@ and routes:
//   - `gh pr view <n> --json isDraft …` → emits the pre-fetch JSON whose
//     isDraft is controlled by the IRIS_FAKE_GH_ISDRAFT env var ("true"/"false").
//   - `gh pr ready <n>` → emits a success line and exits 0.
//
// The script also appends each invocation's full argv to a single "argv"
// file (one block per call, separated by "---") so tests can assert both
// calls happened.
const fakeGHDispatchReady = `
{
  echo "---"
  for arg in "$@"; do
    printf '%s\n' "$arg"
  done
} >> "$IRIS_FAKE_GH_DIR/argv"

case "$2" in
  view)
    printf '{"isDraft":%s}\n' "${IRIS_FAKE_GH_ISDRAFT:-true}"
    exit 0
    ;;
  ready)
    echo "Pull request #${3} is marked as ready for review"
    exit 0
    ;;
esac
exit 99
`

func TestGHPRReady_DraftBecomesReady(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghready-draft")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHDispatchReady)
	t.Setenv("IRIS_FAKE_GH_ISDRAFT", "true")

	result, err := GHPRReady(context.Background(), client, "task-ready", GHPRReadyOptions{PRNumber: 7})
	if err != nil {
		t.Fatalf("gh pr ready: %v", err)
	}
	if !result.Ready {
		t.Fatal("expected Ready=true")
	}
	if !result.WasDraft {
		t.Fatal("expected WasDraft=true (pre-fetch said isDraft=true)")
	}

	argv := readFakeGHArgv(t, dir)
	if !strings.Contains(argv, "view") {
		t.Fatalf("argv missing pre-fetch 'view' call:\n%s", argv)
	}
	if !strings.Contains(argv, "ready") {
		t.Fatalf("argv missing 'ready' call:\n%s", argv)
	}
	if !strings.Contains(argv, "7") {
		t.Fatalf("argv missing PR number 7:\n%s", argv)
	}
}

func TestGHPRReady_AlreadyReadyIsIdempotent(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghready-already")
	client := stubArgus(t, src, wt)
	writeFakeGH(t, fakeGHDispatchReady)
	t.Setenv("IRIS_FAKE_GH_ISDRAFT", "false")

	result, err := GHPRReady(context.Background(), client, "task-already", GHPRReadyOptions{PRNumber: 11})
	if err != nil {
		t.Fatalf("gh pr ready: %v", err)
	}
	if !result.Ready {
		t.Fatal("expected Ready=true")
	}
	if result.WasDraft {
		t.Fatal("expected WasDraft=false (pre-fetch said isDraft=false)")
	}
}

func TestGHPRReady_GHNonZeroReturnsStructuredError(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghready-err")
	client := stubArgus(t, src, wt)

	// Make the SECOND invocation (`pr ready`) fail; the first (`pr view`)
	// must still succeed so we reach the ready shellout.
	body := `
{
  echo "---"
  for arg in "$@"; do
    printf '%s\n' "$arg"
  done
} >> "$IRIS_FAKE_GH_DIR/argv"

case "$2" in
  view)
    echo '{"isDraft":true}'
    exit 0
    ;;
  ready)
    echo "could not mark PR #99 as ready: permission denied" 1>&2
    exit 1
    ;;
esac
exit 99
`
	writeFakeGH(t, body)

	_, err := GHPRReady(context.Background(), client, "task-rerr", GHPRReadyOptions{PRNumber: 99})
	if err == nil {
		t.Fatal("expected error from non-zero gh exit, got nil")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected gh stderr in error, got: %v", err)
	}
}

func TestGHPRReady_RefusesUnknownTaskID(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t)
	_, err := GHPRReady(context.Background(), client, "ghost-task", GHPRReadyOptions{PRNumber: 1})
	if err == nil {
		t.Fatal("expected error for unknown task, got nil")
	}
	if !strings.Contains(err.Error(), "ghost-task") {
		t.Fatalf("expected error to name task id, got: %v", err)
	}
}

func TestGHPRReady_RefusesNonAllowlistedRepo(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghready-denied")
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

	_, err := GHPRReady(context.Background(), client, "task-denied", GHPRReadyOptions{PRNumber: 1})
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

func TestGHPRReady_RejectsNonPositivePR(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t)
	_, err := GHPRReady(context.Background(), client, "task-x", GHPRReadyOptions{PRNumber: 0})
	if err == nil {
		t.Fatal("expected error for pr_number=0, got nil")
	}
	if !strings.Contains(err.Error(), "pr_number") {
		t.Fatalf("expected pr_number validation error, got: %v", err)
	}
}

func TestGHPRReady_ConcurrentCallsSerialize(t *testing.T) {
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
	g(src, "branch", "argus/ready-a")
	g(src, "branch", "argus/ready-b")
	g(src, "worktree", "add", wt1, "argus/ready-a")
	g(src, "worktree", "add", wt2, "argus/ready-b")

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

	// Each invocation sleeps 75ms (twice: view + ready). If parallel ≈150ms.
	// Serialized ≈300ms.
	body := `
sleep 0.075
case "$2" in
  view)  echo '{"isDraft":true}'; exit 0 ;;
  ready) echo "ready"; exit 0 ;;
esac
exit 99
`
	writeFakeGH(t, body)

	start := time.Now()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = GHPRReady(context.Background(), client, "task-a", GHPRReadyOptions{PRNumber: 1})
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = GHPRReady(context.Background(), client, "task-b", GHPRReadyOptions{PRNumber: 2})
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

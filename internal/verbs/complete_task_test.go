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

	"github.com/anutron/iris/internal/argus"
)

// completeTaskStubArgus answers tasks/projects/status/archive for CompleteTask
// tests. taskStatusRef holds the in-memory status used by GetTask responses;
// SetTaskStatus mutates it under the mutex so a subsequent GetTask reflects
// the change (matters for the idempotency test).
type completeTaskStub struct {
	t              *testing.T
	srv            *httptest.Server
	mu             sync.Mutex
	taskStatus     map[string]string
	archived       map[string]bool
	statusFailures map[string]int // taskID -> # of remaining failures to inject
	archiveFails   bool
	worktreePath   map[string]string
	sourceRepo     string
}

func newCompleteTaskStub(t *testing.T, sourceRepo string) *completeTaskStub {
	t.Helper()
	canon, _ := filepath.EvalSymlinks(sourceRepo)
	if canon == "" {
		canon = sourceRepo
	}
	cs := &completeTaskStub{
		t:              t,
		taskStatus:     map[string]string{},
		archived:       map[string]bool{},
		statusFailures: map[string]int{},
		worktreePath:   map[string]string{},
		sourceRepo:     canon,
	}
	cs.srv = httptest.NewServer(http.HandlerFunc(cs.handle))
	t.Cleanup(cs.srv.Close)
	return cs
}

func (cs *completeTaskStub) client() *argus.Client {
	return argus.New(cs.srv.URL, "stub-token")
}

func (cs *completeTaskStub) registerTask(id, worktree string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.worktreePath[id] = worktree
	cs.taskStatus[id] = "in_progress"
}

func (cs *completeTaskStub) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	cs.mu.Lock()
	defer cs.mu.Unlock()

	switch {
	case r.URL.Path == "/api/projects/full":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"projects": []map[string]any{{"name": "iris-test", "path": cs.sourceRepo}},
		})
	case strings.HasPrefix(r.URL.Path, "/api/tasks/") && strings.HasSuffix(r.URL.Path, "/status"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/tasks/"), "/status")
		if cs.statusFailures[id] > 0 {
			cs.statusFailures[id]--
			http.Error(w, "{\"error\":\"injected status failure\"}", http.StatusInternalServerError)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		cs.taskStatus[id] = body["status"]
		w.WriteHeader(http.StatusOK)
	case strings.HasPrefix(r.URL.Path, "/api/tasks/") && strings.HasSuffix(r.URL.Path, "/archive"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/tasks/"), "/archive")
		if cs.archiveFails {
			http.Error(w, "{\"error\":\"injected archive failure\"}", http.StatusInternalServerError)
			return
		}
		cs.archived[id] = true
		w.WriteHeader(http.StatusOK)
	case strings.HasPrefix(r.URL.Path, "/api/tasks/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
		wt, ok := cs.worktreePath[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":            id,
			"name":          "stub",
			"project":       "iris-test",
			"status":        cs.taskStatus[id],
			"worktree_path": wt,
		})
	default:
		http.NotFound(w, r)
	}
}

func TestCompleteTask_HappyFullPath(t *testing.T) {
	t.Parallel()
	src, wt, bare := setupRepoWithBareAndWorktree(t, "complete-happy")
	stub := newCompleteTaskStub(t, src)
	stub.registerTask("task-complete", wt)
	client := stub.client()

	result, err := CompleteTask(context.Background(), client, "task-complete", CompleteTaskOptions{})
	if err != nil {
		t.Fatalf("complete_task: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected non-fatal error: %q", result.Error)
	}
	want := []string{
		CheckpointMerged,
		CheckpointDefaultBranchPushed,
		CheckpointRemoteTaskBranchDeleted,
		CheckpointTaskMarkedComplete,
		CheckpointTaskArchived,
	}
	if !equalStringSlices(result.Checkpoints, want) {
		t.Fatalf("checkpoints mismatch:\n got %v\nwant %v", result.Checkpoints, want)
	}
	if stub.taskStatus["task-complete"] != "complete" {
		t.Fatalf("expected argus task to be marked complete, got %q", stub.taskStatus["task-complete"])
	}
	if !stub.archived["task-complete"] {
		t.Fatal("expected argus task to be archived")
	}
	// Bare origin should have the merged main commit (one new commit beyond initial).
	out := remoteRef(t, bare, "main")
	if out == "" {
		t.Fatal("origin/main not present after push")
	}
	// Remote task branch must be gone.
	if remoteRef(t, bare, "argus/complete-happy") != "" {
		t.Fatal("expected remote task branch deleted")
	}
}

func TestCompleteTask_AlreadyCompleteIsIdempotent(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "complete-already")
	stub := newCompleteTaskStub(t, src)
	stub.registerTask("task-done", wt)
	stub.taskStatus["task-done"] = "complete"
	client := stub.client()

	result, err := CompleteTask(context.Background(), client, "task-done", CompleteTaskOptions{})
	if err != nil {
		t.Fatalf("complete_task on already-complete task: %v", err)
	}
	if len(result.Checkpoints) != 5 {
		t.Fatalf("expected 5 checkpoints, got %v", result.Checkpoints)
	}
}

func TestCompleteTask_PartialFailureReturnsCheckpoints(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "complete-partial")
	stub := newCompleteTaskStub(t, src)
	stub.registerTask("task-partial", wt)
	// Make the SetTaskStatus call fail once. Merge + push + delete remote
	// branch all succeed; status fails; archive never runs.
	stub.statusFailures["task-partial"] = 5
	client := stub.client()

	result, err := CompleteTask(context.Background(), client, "task-partial", CompleteTaskOptions{})
	if err == nil {
		t.Fatal("expected error when status update fails, got nil")
	}
	want := []string{
		CheckpointMerged,
		CheckpointDefaultBranchPushed,
		CheckpointRemoteTaskBranchDeleted,
	}
	if !equalStringSlices(result.Checkpoints, want) {
		t.Fatalf("checkpoints mismatch:\n got %v\nwant %v", result.Checkpoints, want)
	}
}

func TestCompleteTask_ResumeAfterPartialSucceeds(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "complete-resume")
	stub := newCompleteTaskStub(t, src)
	stub.registerTask("task-resume", wt)
	// First attempt: status update fails once. Merge, push default, and
	// remote-branch delete all succeed before the failure.
	stub.statusFailures["task-resume"] = 1
	client := stub.client()

	if _, err := CompleteTask(context.Background(), client, "task-resume", CompleteTaskOptions{}); err == nil {
		t.Fatal("expected first invocation to fail at status step")
	}
	// Second attempt: the task branch is already merged into default, so
	// MergeToMaster's `git merge --no-ff <branch>` is a no-op ("Already up
	// to date."). All five checkpoints should appear and the verb returns
	// success.
	result, err := CompleteTask(context.Background(), client, "task-resume", CompleteTaskOptions{})
	if err != nil {
		t.Fatalf("expected resume to succeed, got: %v", err)
	}
	if len(result.Checkpoints) != 5 {
		t.Fatalf("expected 5 checkpoints on resume, got %v", result.Checkpoints)
	}
}

func TestCompleteTask_ArchiveFailureReturnsSuccessWithError(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "complete-arch")
	stub := newCompleteTaskStub(t, src)
	stub.registerTask("task-arch", wt)
	stub.archiveFails = true
	client := stub.client()

	result, err := CompleteTask(context.Background(), client, "task-arch", CompleteTaskOptions{})
	if err != nil {
		t.Fatalf("expected archive failure to be non-fatal, got: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected best-effort archive failure to surface in result.Error")
	}
	want := []string{
		CheckpointMerged,
		CheckpointDefaultBranchPushed,
		CheckpointRemoteTaskBranchDeleted,
		CheckpointTaskMarkedComplete,
	}
	if !equalStringSlices(result.Checkpoints, want) {
		t.Fatalf("checkpoints mismatch:\n got %v\nwant %v", result.Checkpoints, want)
	}
	if stub.taskStatus["task-arch"] != "complete" {
		t.Fatalf("task should still be marked complete, got %q", stub.taskStatus["task-arch"])
	}
}

func TestCompleteTask_RejectsInvalidMergeStrategy(t *testing.T) {
	t.Parallel()
	src, wt, _ := setupRepoWithBareAndWorktree(t, "complete-bad-strategy")
	stub := newCompleteTaskStub(t, src)
	stub.registerTask("task-bad", wt)
	_, err := CompleteTask(context.Background(), stub.client(), "task-bad", CompleteTaskOptions{MergeStrategy: "rebase"})
	if err == nil {
		t.Fatal("expected error for invalid strategy")
	}
	if !strings.Contains(err.Error(), "merge_strategy") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

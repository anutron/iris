package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anutron/iris/internal/verbs"
)

func intp(i int) *int { return &i }

func TestClampRunWait(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   *int
		want time.Duration
	}{
		{nil, 20 * time.Second},
		{intp(0), 0},
		{intp(5), 5 * time.Second},
		{intp(120), 25 * time.Second},
		{intp(-3), 0},
	}
	for _, c := range cases {
		if got := ClampRunWait(c.in); got != c.want {
			t.Errorf("ClampRunWait(%v) = %s, want %s", c.in, got, c.want)
		}
	}
}

func decodeRunning(t *testing.T, resp Response) map[string]any {
	t.Helper()
	if resp.IsError {
		t.Fatalf("running handle must not be an error: %+v", resp)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(resp.Content[0].Text), &m); err != nil {
		t.Fatalf("decode: %v\n%s", err, resp.Content[0].Text)
	}
	if m["status"] != "running" && m["status"] != "queued" {
		t.Fatalf("expected status running or queued, got %v", m)
	}
	return m
}

// Worktree-runs scenario: fast run answers inline with today's response.
func TestRunRegistry_FastRunAnswersInline(t *testing.T) {
	t.Parallel()
	reg := NewRunRegistry()
	run := reg.Start(context.Background(), "iris_run_checks", "T", "lint", func(context.Context) Response {
		return TextResponse(`{"command":"script/iris-check lint","exit_code":0}`)
	})
	got := reg.Respond(context.Background(), run, 5*time.Second)
	if got.Content[0].Text != `{"command":"script/iris-check lint","exit_code":0}` {
		t.Fatalf("finished run must return the verb's response verbatim, got %q", got.Content[0].Text)
	}
}

// Worktree-runs scenarios: slow run returns a handle, keeps running, and a
// poll returns the final (error) response.
func TestRunRegistry_SlowRunReturnsHandleThenResult(t *testing.T) {
	t.Parallel()
	reg := NewRunRegistry()
	release := make(chan struct{})
	run := reg.Start(context.Background(), "iris_run_checks", "T", "test", func(context.Context) Response {
		<-release
		return ErrorResponse("iris:run_checks: script/iris-check test exited 1\n3 failing")
	})

	handle := decodeRunning(t, reg.Respond(context.Background(), run, 50*time.Millisecond))
	if handle["run_id"] != run.ID || handle["task_id"] != "T" || handle["tool"] != "iris_run_checks" {
		t.Fatalf("handle fields wrong: %v", handle)
	}
	if !strings.Contains(handle["next"].(string), "iris_run_result") {
		t.Fatalf("handle should point at iris_run_result: %v", handle["next"])
	}

	// Poll before completion: still the handle, same run.
	again := reg.Get(run.ID)
	if again != run {
		t.Fatal("Get should return the same run")
	}
	decodeRunning(t, reg.Respond(context.Background(), again, 10*time.Millisecond))

	close(release)
	final := reg.Respond(context.Background(), reg.Get(run.ID), 5*time.Second)
	if !final.IsError || !strings.Contains(final.Content[0].Text, "exited 1\n3 failing") {
		t.Fatalf("expected the stored error response, got %+v", final)
	}
}

// Worktree-runs scenario: the run is detached from the starting call.
func TestRunRegistry_RunOutlivesCallerCancellation(t *testing.T) {
	t.Parallel()
	reg := NewRunRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	sawCancel := make(chan bool, 1)
	run := reg.Start(ctx, "iris_run_build", "T", "", func(runCtx context.Context) Response {
		time.Sleep(100 * time.Millisecond)
		sawCancel <- runCtx.Err() != nil
		return TextResponse("{}")
	})
	cancel()
	reg.Respond(context.Background(), run, 5*time.Second)
	if <-sawCancel {
		t.Fatal("run context was cancelled with the caller")
	}
}

// Worktree-runs scenario: a retry joins the in-progress run.
func TestRunRegistry_DuplicateJoinsInProgressRun(t *testing.T) {
	t.Parallel()
	reg := NewRunRegistry()
	var starts atomic.Int32
	release := make(chan struct{})
	fn := func(context.Context) Response {
		starts.Add(1)
		<-release
		return TextResponse("{}")
	}
	a := reg.Start(context.Background(), "iris_run_checks", "T", "test", fn)
	b := reg.Start(context.Background(), "iris_run_checks", "T", "test", fn)
	other := reg.Start(context.Background(), "iris_run_checks", "T", "lint", fn)
	if a.ID != b.ID {
		t.Fatalf("duplicate call should join: %s vs %s", a.ID, b.ID)
	}
	if other.ID == a.ID {
		t.Fatal("a different check must be a different run")
	}
	close(release)
	reg.Respond(context.Background(), a, 5*time.Second)
	reg.Respond(context.Background(), other, 5*time.Second)
	if n := starts.Load(); n != 2 {
		t.Fatalf("fn started %d times, want 2", n)
	}

	// Once finished, the same key starts a fresh run.
	c := reg.Start(context.Background(), "iris_run_checks", "T", "test", func(context.Context) Response { return TextResponse("{}") })
	if c.ID == a.ID {
		t.Fatal("finished run must not be joined")
	}
}

// Worktree-runs scenario: finished runs expire after the retention window.
func TestRunRegistry_FinishedRunsExpire(t *testing.T) {
	t.Parallel()
	reg := NewRunRegistry()
	var mu sync.Mutex
	clock := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	reg.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	advance := func(d time.Duration) { mu.Lock(); clock = clock.Add(d); mu.Unlock() }

	run := reg.Start(context.Background(), "iris_run_checks", "T", "lint", func(context.Context) Response { return TextResponse("{}") })
	reg.Respond(context.Background(), run, 5*time.Second)

	advance(10 * time.Minute)
	if reg.Get(run.ID) == nil {
		t.Fatal("run finished 10 minutes ago should still be retrievable")
	}
	advance(time.Hour)
	if reg.Get(run.ID) != nil {
		t.Fatal("run should expire an hour after finishing")
	}
}

func TestRunRegistry_PanicBecomesErrorResponse(t *testing.T) {
	t.Parallel()
	reg := NewRunRegistry()
	run := reg.Start(context.Background(), "iris_run_checks", "T", "x", func(context.Context) Response { panic("boom") })
	got := reg.Respond(context.Background(), run, 5*time.Second)
	if !got.IsError || !strings.Contains(got.Content[0].Text, "boom") {
		t.Fatalf("got %+v", got)
	}
}

// Every response for a run names the repo the run resolved, including the
// running handle and later polls from a different request.
func TestRunRegistry_RepoRecordedOnEveryResponse(t *testing.T) {
	t.Parallel()
	reg := NewRunRegistry()
	resolved := make(chan struct{})
	release := make(chan struct{})
	run := reg.Start(context.Background(), "iris_run_checks", "T", "test", func(ctx context.Context) Response {
		verbs.RecordRepo(ctx, &verbs.RepoRef{Name: "acme/video-gen", Path: "/src"})
		close(resolved)
		<-release
		return TextResponse("{}")
	})
	<-resolved

	ctx1, rec1 := verbs.WithRepoRecorder(context.Background())
	decodeRunning(t, reg.Respond(ctx1, run, 10*time.Millisecond))
	if ref := rec1.Ref(); ref == nil || ref.Name != "acme/video-gen" {
		t.Fatalf("running handle not attributed: %+v", ref)
	}

	close(release)
	ctx2, rec2 := verbs.WithRepoRecorder(context.Background())
	reg.Respond(ctx2, reg.Get(run.ID), 5*time.Second)
	if ref := rec2.Ref(); ref == nil || ref.Name != "acme/video-gen" {
		t.Fatalf("polled result not attributed: %+v", ref)
	}
}

// iris_run_result scenario: unknown run_id.
func TestRunResultHandler_UnknownRunID(t *testing.T) {
	t.Parallel()
	h := NewRunResultHandler(NewRunRegistry())
	got := h.Handle(context.Background(), json.RawMessage(`{"run_id":"deadbeef"}`))
	if !got.IsError {
		t.Fatal("expected error")
	}
	for _, want := range []string{"deadbeef", "restarted", "re-run"} {
		if !strings.Contains(got.Content[0].Text, want) {
			t.Fatalf("error should mention %q: %s", want, got.Content[0].Text)
		}
	}
}

func TestRunResultHandler_RequiresRunID(t *testing.T) {
	t.Parallel()
	got := NewRunResultHandler(NewRunRegistry()).Handle(context.Background(), json.RawMessage(`{}`))
	if !got.IsError || !strings.Contains(got.Content[0].Text, "run_id") {
		t.Fatalf("got %+v", got)
	}
}

func TestRunResultHandler_ReturnsFinishedResponse(t *testing.T) {
	t.Parallel()
	reg := NewRunRegistry()
	run := reg.Start(context.Background(), "iris_run_build", "T", "", func(context.Context) Response {
		return TextResponse(`{"command":"make build","exit_code":0}`)
	})
	got := NewRunResultHandler(reg).Handle(context.Background(), json.RawMessage(`{"run_id":"`+run.ID+`","wait_seconds":5}`))
	if got.IsError || got.Content[0].Text != `{"command":"make build","exit_code":0}` {
		t.Fatalf("got %+v", got)
	}
}

// A run waiting for the worktree lock reports "queued", then "running".
func TestRunRegistry_QueuedUntilStarted(t *testing.T) {
	t.Parallel()
	reg := NewRunRegistry()
	start, release := make(chan struct{}), make(chan struct{})
	run := reg.Start(context.Background(), "iris_run_checks", "T", "test", func(ctx context.Context) Response {
		<-start
		verbs.MarkRunStarted(ctx)
		<-release
		return TextResponse("{}")
	})
	if got := decodeRunning(t, reg.Respond(context.Background(), run, 0)); got["status"] != "queued" {
		t.Fatalf("before the lock: status %v, want queued", got["status"])
	}
	close(start)
	deadline := time.Now().Add(5 * time.Second)
	for !run.started.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := decodeRunning(t, reg.Respond(context.Background(), run, 0)); got["status"] != "running" {
		t.Fatalf("after the lock: status %v, want running", got["status"])
	}
	close(release)
}

// A handle returned before the run resolves (wait_seconds=0) still names
// the repo the handler resolved.
func TestRunRegistry_ImmediateHandleNamesSeededRepo(t *testing.T) {
	t.Parallel()
	reg := NewRunRegistry()
	release := make(chan struct{})
	startCtx, _ := verbs.WithRepoRecorder(context.Background())
	verbs.RecordRepo(startCtx, testRef)
	run := reg.Start(startCtx, "iris_run_checks", "T", "test", func(context.Context) Response {
		<-release
		return TextResponse("{}")
	})
	defer close(release)

	pollCtx, rec := verbs.WithRepoRecorder(context.Background())
	decodeRunning(t, reg.Respond(pollCtx, reg.Get(run.ID), 0))
	if ref := rec.Ref(); ref == nil || *ref != *testRef {
		t.Fatalf("immediate handle not attributed: %+v", ref)
	}
}

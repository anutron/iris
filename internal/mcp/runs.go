package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anutron/iris/internal/verbs"
)

// Argus caps every plugin callback at 30s and iris's own listener has a 30s
// WriteTimeout, so a tool call must answer well inside that. Registered runs
// wait up to MaxRunWaitSeconds, then hand back a run_id for iris_run_result.
const (
	DefaultRunWaitSeconds = 20
	MaxRunWaitSeconds     = 25
	runRetention          = time.Hour
)

// ClampRunWait converts an optional wait_seconds input into the wait to
// apply: DefaultRunWaitSeconds when omitted, clamped to [0, MaxRunWaitSeconds].
func ClampRunWait(waitSeconds *int) time.Duration {
	s := DefaultRunWaitSeconds
	if waitSeconds != nil {
		s = *waitSeconds
	}
	s = max(0, min(s, MaxRunWaitSeconds))
	return time.Duration(s) * time.Second
}

// Run is one in-memory transfer, check or build.
type Run struct {
	ID        string
	Tool      string
	TaskID    string
	Arg       string
	StartedAt time.Time

	key     string
	rec     *verbs.RepoRecorder
	started atomic.Bool // holds its operation's lock and is executing
	done    chan struct{}

	// Set under RunRegistry.mu before done is closed.
	resp       Response
	finishedAt time.Time
}

// RunRegistry tracks runs so a run can outlive the MCP call that
// started it. Runs live in memory only: a daemon restart forgets them.
type RunRegistry struct {
	mu        sync.Mutex
	runs      map[string]*Run
	active    map[string]*Run
	retention time.Duration
	now       func() time.Time
}

// NewRunRegistry returns an empty registry.
func NewRunRegistry() *RunRegistry {
	return &RunRegistry{
		runs:      map[string]*Run{},
		active:    map[string]*Run{},
		retention: runRetention,
		now:       time.Now,
	}
}

// Start launches fn as a run detached from ctx's cancellation, or joins
// the in-progress run for the same (tool, taskID, arg) so a retry doesn't
// queue a second run behind the operation's lock. fn's response is stored
// verbatim and served to every caller of that run. A repo already recorded
// in ctx (the handler resolves before starting) seeds the run's recorder,
// so even a handle returned before fn resolves names the repo.
func (r *RunRegistry) Start(ctx context.Context, tool, taskID, arg string, fn func(ctx context.Context) Response) *Run {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked()

	key := tool + "\x00" + taskID + "\x00" + arg
	if run, ok := r.active[key]; ok {
		return run
	}

	runCtx, rec := verbs.WithRepoRecorder(context.WithoutCancel(ctx))
	verbs.RecordRepo(runCtx, verbs.RecordedRepo(ctx))
	run := &Run{
		ID:        newRunID(),
		Tool:      tool,
		TaskID:    taskID,
		Arg:       arg,
		StartedAt: r.now(),
		key:       key,
		rec:       rec,
		done:      make(chan struct{}),
	}
	runCtx = verbs.WithStartHook(runCtx, func() { run.started.Store(true) })
	r.runs[run.ID] = run
	r.active[key] = run

	go func() {
		var resp Response
		defer func() {
			if p := recover(); p != nil {
				resp = ErrorResponse(fmt.Sprintf("%s: run %s panicked: %v", tool, run.ID, p))
			}
			r.mu.Lock()
			run.resp = resp
			run.finishedAt = r.now()
			delete(r.active, run.key)
			close(run.done)
			r.mu.Unlock()
		}()
		resp = fn(runCtx)
	}()
	return run
}

// Get returns the run with id, or nil when it is unknown or expired.
func (r *RunRegistry) Get(id string) *Run {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked()
	return r.runs[id]
}

// Respond waits up to wait for run, records its repo into ctx (so the
// server names it on this response), and returns the run's final response
// or a "running" handle.
func (r *RunRegistry) Respond(ctx context.Context, run *Run, wait time.Duration) Response {
	finished := run.wait(ctx, wait)
	verbs.RecordRepo(ctx, run.rec.Ref())
	if finished {
		r.mu.Lock()
		defer r.mu.Unlock()
		return run.resp
	}
	return r.runningResponse(run)
}

func (run *Run) wait(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-run.done:
		return true
	case <-timer.C:
	case <-ctx.Done():
	}
	// A run that finished in the same instant still counts.
	select {
	case <-run.done:
		return true
	default:
		return false
	}
}

// runningResponse is the handle for an unfinished run. status is "queued"
// while the run waits for its lock and "running" once it is executing.
func (r *RunRegistry) runningResponse(run *Run) Response {
	status := "queued"
	if run.started.Load() {
		status = "running"
	}
	body, _ := json.MarshalIndent(map[string]any{
		"run_id":          run.ID,
		"status":          status,
		"tool":            run.Tool,
		"task_id":         run.TaskID,
		"started_at":      run.StartedAt.UTC().Format(time.RFC3339),
		"elapsed_seconds": int(r.now().Sub(run.StartedAt).Seconds()),
		"next":            fmt.Sprintf("Not finished (%s). Call iris_run_result(run_id=%q) to wait up to %ds more for the result; repeat until it returns.", status, run.ID, MaxRunWaitSeconds),
	}, "", "  ")
	return TextResponse(string(body))
}

func (r *RunRegistry) pruneLocked() {
	cutoff := r.now().Add(-r.retention)
	for id, run := range r.runs {
		select {
		case <-run.done:
			if run.finishedAt.Before(cutoff) {
				delete(r.runs, id)
			}
		default:
		}
	}
}

func newRunID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

package verbs

import (
	"context"
	"sync"
	"time"
)

// buildLocks holds one *sync.Mutex per WORKTREE absolute path. Builds
// write to the worktree (target/, node_modules/, dist/, etc.), so the
// lock granularity is per-worktree — distinct from the per-source-repo
// `repoLocks` table used by `merge_to_master` and `push`.
//
// Two builds in DIFFERENT worktrees of the same source repo are
// independent and run concurrently. Two builds in the SAME worktree
// serialize cleanly.
var buildLocks sync.Map // map[string]*sync.Mutex

// lockWorktreeCtx acquires the (creating if needed) per-worktree mutex,
// giving up when ctx is done. sync.Mutex has no timed Lock, so it polls
// TryLock; contention here is a second run in the same worktree, so a
// short poll interval costs nothing. The caller MUST Unlock on success.
func lockWorktreeCtx(ctx context.Context, path string) (*sync.Mutex, error) {
	m, _ := buildLocks.LoadOrStore(path, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	for !mu.TryLock() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	return mu, nil
}

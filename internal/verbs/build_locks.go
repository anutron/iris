package verbs

import "sync"

// buildLocks holds one *sync.Mutex per WORKTREE absolute path. Builds
// write to the worktree (target/, node_modules/, dist/, etc.), so the
// lock granularity is per-worktree — distinct from the per-source-repo
// `repoLocks` table used by `merge_to_master` and `push`.
//
// Two builds in DIFFERENT worktrees of the same source repo are
// independent and run concurrently. Two builds in the SAME worktree
// serialize cleanly.
var buildLocks sync.Map // map[string]*sync.Mutex

// lockWorktree returns the (creating if needed) per-worktree mutex,
// already held. The caller MUST defer Unlock.
func lockWorktree(path string) *sync.Mutex {
	m, _ := buildLocks.LoadOrStore(path, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu
}

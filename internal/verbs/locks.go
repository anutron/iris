package verbs

import "sync"

// repoLocks holds one *sync.Mutex per source-repo absolute path. Verbs
// that mutate a source repo hold the mutex for the full operation so
// concurrent calls from different tasks against the same source repo
// serialize cleanly.
//
// The map itself is package-private so all verbs share the same lock
// table — calling MergeToMaster and a future Push concurrently against
// the same repo will also serialize, which is the intent (both touch
// master).
var repoLocks sync.Map // map[string]*sync.Mutex

// lockSourceRepo returns the (creating if needed) per-repo mutex,
// already held. The caller MUST defer Unlock.
func lockSourceRepo(path string) *sync.Mutex {
	m, _ := repoLocks.LoadOrStore(path, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu
}

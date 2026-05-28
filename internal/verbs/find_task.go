package verbs

import (
	"context"

	"github.com/anutron/iris/internal/argus"
)

// FindTaskBySourceRepo asks argus for its full task list and returns the
// first task whose canonicalized worktree_path equals the given source
// repo path. Returns (nil, nil) when no task matches – that is NOT an
// error condition. Returns (nil, err) only when argus itself errors.
//
// Iris uses this for "reverse lookup": given a source repo path, find
// the argus task (if any) that points at it. Echoing the resolved task
// in status output lets consumers (humans, argus agents) see what task
// iris associates with this repo without needing a separate call.
func FindTaskBySourceRepo(ctx context.Context, client *argus.Client, sourceRepo string) (*argus.Task, error) {
	if client == nil || sourceRepo == "" {
		return nil, nil
	}
	tasks, err := client.ListTasks(ctx)
	if err != nil {
		return nil, err
	}
	for i := range tasks {
		t := &tasks[i]
		if t.WorktreePath == "" {
			continue
		}
		if EqualSourceRepos(t.WorktreePath, sourceRepo) {
			return t, nil
		}
	}
	return nil, nil
}

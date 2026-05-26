package verbs

import (
	"context"
	"fmt"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// PushOptions captures the per-call knobs for Push.
type PushOptions struct {
	// ForceWithLease appends --force-with-lease to git push when true.
	ForceWithLease bool
}

// PushResult is the structured success payload.
type PushResult struct {
	Pushed    bool   `json:"pushed"`
	Branch    string `json:"branch"`
	RemoteSHA string `json:"remote_sha"`
}

// Push pushes the task's branch to origin from the source repo. The
// per-source-repo mutex protects against concurrent git mutations.
//
// Safety: refuses to push the default branch (master/main). The verb is
// narrow on purpose — tag pushes and other-branch pushes are out of scope.
func Push(ctx context.Context, client *argus.Client, taskID string, opts PushOptions) (*PushResult, error) {
	resolved, err := Resolve(ctx, client, taskID)
	if err != nil {
		return nil, err
	}

	defaultBranch, err := DefaultBranch(ctx, resolved.SourceRepo)
	if err != nil {
		return nil, fmt.Errorf("determine default branch: %w", err)
	}

	if resolved.Branch == "" {
		return nil, fmt.Errorf("task has no current branch")
	}
	if resolved.Branch == defaultBranch {
		return nil, fmt.Errorf("refusing to push default branch %q", resolved.Branch)
	}

	mu := lockSourceRepo(resolved.SourceRepo)
	defer mu.Unlock()

	pushArgs := []string{"push", "origin", resolved.Branch}
	if opts.ForceWithLease {
		pushArgs = append(pushArgs, "--force-with-lease")
	}
	if out, err := runGit(ctx, resolved.SourceRepo, pushArgs...); err != nil {
		return nil, fmt.Errorf("push %s: %w; log:\n%s", resolved.Branch, err, out)
	}

	sha, err := runGit(ctx, resolved.SourceRepo, "rev-parse", "origin/"+resolved.Branch)
	if err != nil {
		return nil, fmt.Errorf("rev-parse origin/%s: %w", resolved.Branch, err)
	}

	return &PushResult{
		Pushed:    true,
		Branch:    resolved.Branch,
		RemoteSHA: strings.TrimSpace(sha),
	}, nil
}

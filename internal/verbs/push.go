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
	// Branch, when non-empty, overrides the task's resolved branch. The
	// effective branch is this value; the default-branch refusal applies.
	Branch string
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

	// Compute the effective branch: use the override when provided, otherwise
	// fall back to the task's resolved branch.
	effective := resolved.Branch
	if opts.Branch != "" {
		effective = opts.Branch
	}

	if effective == "" {
		return nil, fmt.Errorf("task has no current branch")
	}
	// Reject a leading '-' so a caller-supplied branch override cannot smuggle
	// flags into git (e.g. `--upload-pack=evil`). Real branch names never begin
	// with `-`.
	if strings.HasPrefix(effective, "-") {
		return nil, fmt.Errorf("invalid branch name %q (must not begin with '-')", effective)
	}
	if effective == defaultBranch {
		return nil, fmt.Errorf("refusing to push default branch %q", effective)
	}

	mu := lockSourceRepo(resolved.SourceRepo)
	defer mu.Unlock()

	pushArgs := []string{"push", "origin", effective}
	if opts.ForceWithLease {
		pushArgs = append(pushArgs, "--force-with-lease")
	}
	if out, err := runGit(ctx, resolved.SourceRepo, pushArgs...); err != nil {
		return nil, fmt.Errorf("push %s: %w; log:\n%s", effective, err, out)
	}

	sha, err := runGit(ctx, resolved.SourceRepo, "rev-parse", "origin/"+effective)
	if err != nil {
		return nil, fmt.Errorf("rev-parse origin/%s: %w", effective, err)
	}

	return &PushResult{
		Pushed:    true,
		Branch:    effective,
		RemoteSHA: strings.TrimSpace(sha),
	}, nil
}

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
	// Remote, when non-empty, overrides the default "origin" target. The
	// effective remote MUST be a remote already configured in the source
	// repo — Push validates it exists and never accepts a URL.
	Remote string
}

// PushResult is the structured success payload.
type PushResult struct {
	Pushed    bool   `json:"pushed"`
	Branch    string `json:"branch"`
	Remote    string `json:"remote"`
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

	// Compute the effective remote: use the override when provided, otherwise
	// fall back to origin.
	remote := "origin"
	if opts.Remote != "" {
		remote = opts.Remote
	}
	// Reject a leading '-' so a caller-supplied remote override cannot smuggle
	// flags into git. Real remote names never begin with `-`.
	if strings.HasPrefix(remote, "-") {
		return nil, fmt.Errorf("invalid remote name %q (must not begin with '-')", remote)
	}

	mu := lockSourceRepo(resolved.SourceRepo)
	defer mu.Unlock()

	// The remote must already be configured in the source repo. iris pushes to
	// named remotes only — never to ad-hoc URLs — so resolve it up front and
	// fail cleanly when it is unknown.
	if _, err := runGit(ctx, resolved.SourceRepo, "remote", "get-url", remote); err != nil {
		return nil, fmt.Errorf("unknown git remote %q in source repo %s", remote, resolved.SourceRepo)
	}

	// Push resolved.Branch (the task's actual, always-current work) as the
	// source, naming it effective (the possibly-overridden name) on the
	// remote. A bare `push <remote> <effective>` would instead read
	// whatever local ref is literally named <effective> in the shared
	// source repo — which, under a branch= override, can be a stale,
	// unrelated ref left over from other tasks, silently publishing the
	// wrong commit while still reporting success.
	refspec := resolved.Branch + ":" + effective
	pushArgs := []string{"push", remote, refspec}
	if opts.ForceWithLease {
		pushArgs = append(pushArgs, "--force-with-lease")
	}
	if out, err := runGit(ctx, resolved.SourceRepo, pushArgs...); err != nil {
		return nil, fmt.Errorf("push %s to %s: %w; log:\n%s", effective, remote, err, out)
	}

	sha, err := runGit(ctx, resolved.SourceRepo, "rev-parse", remote+"/"+effective)
	if err != nil {
		return nil, fmt.Errorf("rev-parse %s/%s: %w", remote, effective, err)
	}

	return &PushResult{
		Pushed:    true,
		Branch:    effective,
		Remote:    remote,
		RemoteSHA: strings.TrimSpace(sha),
	}, nil
}

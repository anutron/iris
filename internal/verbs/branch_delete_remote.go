package verbs

import (
	"context"
	"fmt"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// BranchDeleteRemoteInput captures the typed arguments for
// BranchDeleteRemote.
type BranchDeleteRemoteInput struct {
	Client *argus.Client
	TaskID string
	Branch string
}

// BranchDeleteRemoteResult is the structured success payload.
type BranchDeleteRemoteResult struct {
	Deleted        bool   `json:"deleted"`
	Branch         string `json:"branch"`
	PriorRemoteSHA string `json:"prior_remote_sha"`
}

// BranchDeleteRemote runs `git push origin :<branch>` in the resolved
// source repo under the per-source-repo lock. Refuses the default
// branch and any branch that doesn't currently exist on origin.
func BranchDeleteRemote(ctx context.Context, in BranchDeleteRemoteInput) (*BranchDeleteRemoteResult, error) {
	if in.Branch == "" {
		return nil, fmt.Errorf("branch is required")
	}

	resolved, err := Resolve(ctx, in.Client, in.TaskID)
	if err != nil {
		return nil, err
	}

	defaultBranch, err := DefaultBranch(ctx, resolved.SourceRepo)
	if err != nil {
		return nil, fmt.Errorf("determine default branch: %w", err)
	}
	if in.Branch == defaultBranch {
		return nil, fmt.Errorf("refusing to delete default branch %q (default: %q)", in.Branch, defaultBranch)
	}

	mu := lockSourceRepo(resolved.SourceRepo)
	defer mu.Unlock()

	priorSHA, err := remoteBranchSHA(ctx, resolved.SourceRepo, in.Branch)
	if err != nil {
		return nil, err
	}
	if priorSHA == "" {
		return nil, fmt.Errorf("branch %q does not exist on origin", in.Branch)
	}

	if out, err := runGit(ctx, resolved.SourceRepo, "push", "origin", ":"+in.Branch); err != nil {
		return nil, fmt.Errorf("push origin :%s: %w; log:\n%s", in.Branch, err, out)
	}

	return &BranchDeleteRemoteResult{
		Deleted:        true,
		Branch:         in.Branch,
		PriorRemoteSHA: priorSHA,
	}, nil
}

// remoteBranchSHA returns origin/<branch>'s SHA via
// `git ls-remote --heads origin <branch>`, or "" when the branch is
// absent from origin. Errors propagate (network / auth failures must
// not be mistaken for absence).
func remoteBranchSHA(ctx context.Context, sourceRepo, branch string) (string, error) {
	out, err := runGit(ctx, sourceRepo, "ls-remote", "--heads", "origin", branch)
	if err != nil {
		return "", fmt.Errorf("ls-remote --heads origin %s: %w", branch, err)
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return "", nil
	}
	// `ls-remote` output: "<sha>\t<ref>". One line per match.
	fields := strings.Fields(trimmed)
	if len(fields) < 1 {
		return "", nil
	}
	return fields[0], nil
}

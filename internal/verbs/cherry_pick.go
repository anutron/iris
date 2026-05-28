package verbs

import (
	"context"
	"fmt"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// CherryPickInput captures the typed arguments for CherryPick.
type CherryPickInput struct {
	Client       *argus.Client
	TaskID       string
	Commit       string
	TargetBranch string
}

// CherryPickResult is the structured success payload.
type CherryPickResult struct {
	CherryPicked bool   `json:"cherry_picked"`
	Commit       string `json:"commit"`
	TargetBranch string `json:"target_branch"`
	NewSHA       string `json:"new_sha"`
}

// CherryPick checks out target_branch in the resolved source repo and
// applies commit via `git cherry-pick`. On conflict, the verb runs
// `git cherry-pick --abort` and returns a structured error carrying the
// conflict paths; the source repo lands back on target_branch with a clean
// working tree.
//
// Safety: refuses empty values, leading-dash inputs (argv-flag smuggling),
// the default branch as target (use iris:merge_to_master for landing on
// master/main), unknown target branches, and unresolvable commits. Holds
// the per-source-repo lock across the checkout + cherry-pick pair so the
// composite is atomic from other iris callers.
func CherryPick(ctx context.Context, in CherryPickInput) (*CherryPickResult, error) {
	if in.Commit == "" {
		return nil, fmt.Errorf("commit is required")
	}
	if in.TargetBranch == "" {
		return nil, fmt.Errorf("target_branch is required")
	}
	if strings.HasPrefix(in.Commit, "-") {
		return nil, fmt.Errorf("invalid commit %q (must not begin with '-')", in.Commit)
	}
	if strings.HasPrefix(in.TargetBranch, "-") {
		return nil, fmt.Errorf("invalid target_branch %q (must not begin with '-')", in.TargetBranch)
	}

	resolved, err := Resolve(ctx, in.Client, in.TaskID)
	if err != nil {
		return nil, err
	}

	defaultBranch, err := DefaultBranch(ctx, resolved.SourceRepo)
	if err != nil {
		return nil, fmt.Errorf("determine default branch: %w", err)
	}
	if in.TargetBranch == defaultBranch || in.TargetBranch == "main" || in.TargetBranch == "master" {
		return nil, fmt.Errorf("refusing to cherry-pick onto default branch %q (use iris:merge_to_master instead)", in.TargetBranch)
	}

	mu := lockSourceRepo(resolved.SourceRepo)
	defer mu.Unlock()

	if _, err := runGit(ctx, resolved.SourceRepo, "rev-parse", "--verify", "--quiet", "refs/heads/"+in.TargetBranch); err != nil {
		return nil, fmt.Errorf("target_branch %q does not exist locally", in.TargetBranch)
	}
	if _, err := runGit(ctx, resolved.SourceRepo, "rev-parse", "--verify", "--quiet", in.Commit+"^{commit}"); err != nil {
		return nil, fmt.Errorf("commit %q does not resolve", in.Commit)
	}

	if out, err := runGit(ctx, resolved.SourceRepo, "checkout", in.TargetBranch); err != nil {
		return nil, fmt.Errorf("checkout %s: %w; log:\n%s", in.TargetBranch, err, out)
	}

	if out, err := runGit(ctx, resolved.SourceRepo, "cherry-pick", in.Commit); err != nil {
		conflicts := listGitPaths(ctx, resolved.SourceRepo, "diff", "--name-only", "--diff-filter=U")
		if _, abortErr := runGit(ctx, resolved.SourceRepo, "cherry-pick", "--abort"); abortErr != nil {
			return nil, fmt.Errorf("cherry-pick %s failed and abort failed: %w (abort: %v); log:\n%s",
				in.Commit, err, abortErr, out)
		}
		return nil, fmt.Errorf("cherry-pick %s onto %s failed (aborted); conflicts: %s; log:\n%s",
			in.Commit, in.TargetBranch, strings.Join(conflicts, ", "), out)
	}

	newSHA, err := runGit(ctx, resolved.SourceRepo, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("read new HEAD: %w", err)
	}

	return &CherryPickResult{
		CherryPicked: true,
		Commit:       in.Commit,
		TargetBranch: in.TargetBranch,
		NewSHA:       strings.TrimSpace(newSHA),
	}, nil
}

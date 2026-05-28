package verbs

import (
	"context"
	"fmt"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// BranchCreateInput captures the typed arguments for BranchCreate.
type BranchCreateInput struct {
	Client  *argus.Client
	TaskID  string
	Name    string
	BaseRef string
}

// BranchCreateResult is the structured success payload.
type BranchCreateResult struct {
	Created bool   `json:"created"`
	Branch  string `json:"branch"`
	BaseRef string `json:"base_ref"`
	SHA     string `json:"sha"`
}

// BranchCreate creates a branch in the resolved source repo from an
// arbitrary ref. It does NOT change the source repo's current checkout —
// composing with iris:checkout is the explicit "create + switch" path.
//
// Safety: refuses empty values, refnames starting with `-` (argv-flag
// smuggling), the default branch name (and main/master regardless of
// origin/HEAD), invalid git refs (per check-ref-format), and pre-existing
// local branches.
func BranchCreate(ctx context.Context, in BranchCreateInput) (*BranchCreateResult, error) {
	if in.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if in.BaseRef == "" {
		return nil, fmt.Errorf("base_ref is required")
	}
	if strings.HasPrefix(in.Name, "-") {
		return nil, fmt.Errorf("invalid branch name %q (must not begin with '-')", in.Name)
	}
	if strings.HasPrefix(in.BaseRef, "-") {
		return nil, fmt.Errorf("invalid base_ref %q (must not begin with '-')", in.BaseRef)
	}

	resolved, err := Resolve(ctx, in.Client, in.TaskID)
	if err != nil {
		return nil, err
	}

	defaultBranch, err := DefaultBranch(ctx, resolved.SourceRepo)
	if err != nil {
		return nil, fmt.Errorf("determine default branch: %w", err)
	}
	if in.Name == defaultBranch || in.Name == "main" || in.Name == "master" {
		return nil, fmt.Errorf("refusing to create default branch %q (default: %q)", in.Name, defaultBranch)
	}

	mu := lockSourceRepo(resolved.SourceRepo)
	defer mu.Unlock()

	if _, err := runGit(ctx, resolved.SourceRepo, "check-ref-format", "--branch", in.Name); err != nil {
		return nil, fmt.Errorf("invalid branch name %q (git check-ref-format rejected it)", in.Name)
	}

	if existing, err := runGit(ctx, resolved.SourceRepo, "rev-parse", "--verify", "--quiet", "refs/heads/"+in.Name); err == nil {
		return nil, fmt.Errorf("branch %q already exists locally (sha: %s)", in.Name, strings.TrimSpace(existing))
	}

	if out, err := runGit(ctx, resolved.SourceRepo, "branch", in.Name, in.BaseRef); err != nil {
		return nil, fmt.Errorf("create branch %s from %s: %w; log:\n%s", in.Name, in.BaseRef, err, out)
	}

	sha, err := runGit(ctx, resolved.SourceRepo, "rev-parse", "refs/heads/"+in.Name)
	if err != nil {
		return nil, fmt.Errorf("read created branch sha: %w", err)
	}

	return &BranchCreateResult{
		Created: true,
		Branch:  in.Name,
		BaseRef: in.BaseRef,
		SHA:     strings.TrimSpace(sha),
	}, nil
}

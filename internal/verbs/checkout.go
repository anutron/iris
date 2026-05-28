package verbs

import (
	"context"
	"fmt"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// CheckoutInput captures the typed arguments for Checkout.
type CheckoutInput struct {
	Client *argus.Client
	TaskID string
	Branch string
	// Force enables the "get me unstuck" recovery path: best-effort abort
	// of any in-progress merge/cherry-pick/rebase, then `git checkout -f`.
	// Uncommitted changes are discarded.
	Force bool
}

// CheckoutResult is the structured success payload.
type CheckoutResult struct {
	CheckedOut  bool   `json:"checked_out"`
	Branch      string `json:"branch"`
	HeadSHA     string `json:"head_sha"`
	PriorBranch string `json:"prior_branch"`
	PriorHead   string `json:"prior_head"`
}

// Checkout switches the resolved source repo to branch. With Force=false,
// it's plain `git checkout <branch>` — git's own refusal for dirty trees
// or in-progress merges/cherry-picks propagates. With Force=true, the
// verb first aborts any in-progress merge/cherry-pick/rebase (best-effort),
// then runs `git checkout -f`, discarding any uncommitted changes.
//
// PriorBranch and PriorHead reflect the state observed BEFORE any
// recovery or checkout, so callers can recover the discarded SHA via
// reflog if Force=true was a misfire.
func Checkout(ctx context.Context, in CheckoutInput) (*CheckoutResult, error) {
	if in.Branch == "" {
		return nil, fmt.Errorf("branch is required")
	}
	if strings.HasPrefix(in.Branch, "-") {
		return nil, fmt.Errorf("invalid branch name %q (must not begin with '-')", in.Branch)
	}

	resolved, err := Resolve(ctx, in.Client, in.TaskID)
	if err != nil {
		return nil, err
	}

	mu := lockSourceRepo(resolved.SourceRepo)
	defer mu.Unlock()

	// Capture prior state BEFORE any recovery so the caller can audit
	// what was discarded on a force-checkout misfire.
	priorBranch := ""
	if out, err := runGit(ctx, resolved.SourceRepo, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		b := strings.TrimSpace(out)
		if b != "HEAD" { // detached HEAD; leave PriorBranch empty
			priorBranch = b
		}
	}
	priorHead := ""
	if out, err := runGit(ctx, resolved.SourceRepo, "rev-parse", "HEAD"); err == nil {
		priorHead = strings.TrimSpace(out)
	}

	if in.Force {
		// Best-effort recovery from in-progress operations. Each abort
		// silently no-ops when the corresponding state file is absent;
		// errors are swallowed because we're about to `checkout -f`
		// anyway and the caller asked for recovery.
		_, _ = runGit(ctx, resolved.SourceRepo, "merge", "--abort")
		_, _ = runGit(ctx, resolved.SourceRepo, "cherry-pick", "--abort")
		_, _ = runGit(ctx, resolved.SourceRepo, "rebase", "--abort")
		if out, err := runGit(ctx, resolved.SourceRepo, "checkout", "-f", in.Branch); err != nil {
			return nil, fmt.Errorf("force checkout %s: %w; log:\n%s", in.Branch, err, out)
		}
	} else {
		if out, err := runGit(ctx, resolved.SourceRepo, "checkout", in.Branch); err != nil {
			return nil, fmt.Errorf("checkout %s: %w; log:\n%s", in.Branch, err, out)
		}
	}

	headSHA, err := runGit(ctx, resolved.SourceRepo, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("read HEAD: %w", err)
	}

	return &CheckoutResult{
		CheckedOut:  true,
		Branch:      in.Branch,
		HeadSHA:     strings.TrimSpace(headSHA),
		PriorBranch: priorBranch,
		PriorHead:   priorHead,
	}, nil
}

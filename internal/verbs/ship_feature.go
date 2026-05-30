// Package verbs: ship_feature implements iris:ship_feature — the single ship
// motion that lands a feature branch on origin's default branch via a GitHub
// pull request.
//
// Ship is an orchestrator, not a primitive: it composes the existing push and
// gh-pr-create plumbing into one call so the agent can say "ship F2" without
// sequencing several tool calls. Two via modes are designed:
//
//	pr      — push branch + open PR. Stop. The worker returns after review.
//	pr-auto — push + open + wait-for-CI + approve + merge + fetch + re-compose.
//
// This stage implements `pr` ONLY. `pr-auto` (and the post-ship dogfood
// re-compose) is a later stage; an unknown/unsupported via is refused naming
// the modes iris currently supports, so adding `pr-auto` is a one-case change.
//
// See openspec/changes/add-dogfood-and-ship-verbs/design.md
// ("Ship is an orchestrator, not a primitive" + "Two via modes") and
// specs/iris-ship-feature/spec.md.

package verbs

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// ShipFeatureOpts captures the typed arguments for ShipFeature.
type ShipFeatureOpts struct {
	// Branch is the local feature branch to ship. MUST exist locally and MUST
	// NOT be the source repo's default branch.
	Branch string
	// Via selects the ship mode. Only "pr" is supported in this stage.
	Via string
	// PRTitle is the PR title; defaults to the branch's last commit subject
	// when empty.
	PRTitle string
	// PRBody is the optional PR body.
	PRBody string
	// MergeMethod is "squash" (default), "merge", or "rebase". Unused in pr
	// mode — recorded for forward-compat with pr-auto.
	MergeMethod string
}

// Warning is a structured, non-fatal warning. pr mode emits none; pr-auto
// (a later stage) uses these to surface ci_failed / ci_timeout outcomes.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ShipFeatureResult is the structured success payload, mirrored to MCP/CLI as
// pretty-printed JSON.
type ShipFeatureResult struct {
	Shipped  bool      `json:"shipped"`
	Branch   string    `json:"branch"`
	PRNumber int       `json:"pr_number"`
	PRURL    string    `json:"pr_url"`
	Merged   bool      `json:"merged"`   // always false in pr mode
	MergeSHA string    `json:"merge_sha,omitempty"`
	Fetched  bool      `json:"fetched"`  // always false in pr mode
	Warnings []Warning `json:"warnings,omitempty"`
}

// shipSupportedModes is the human-readable list of via modes iris currently
// implements. Stage 6 adds "pr-auto" here and a matching switch case.
const shipSupportedModes = `"pr"`

// ShipFeature ships opts.Branch to origin's default branch via a GitHub PR.
//
// pr mode sequence: validate via -> resolve source repo -> reject default
// branch -> require branch to exist locally -> push branch to origin -> open a
// PR targeting the default branch. It never merges, fetches, or touches the
// dogfood branch/manifest.
//
// All refusals happen before any mutation: an unknown via is rejected before
// resolution, and the default-branch / missing-branch refusals are checked
// before the push.
func ShipFeature(ctx context.Context, client *argus.Client, taskID string, opts ShipFeatureOpts) (*ShipFeatureResult, error) {
	if strings.TrimSpace(opts.Branch) == "" {
		return nil, fmt.Errorf("branch is required")
	}
	if strings.HasPrefix(opts.Branch, "-") {
		return nil, fmt.Errorf("invalid branch %q (must not begin with '-')", opts.Branch)
	}

	// Validate the mode up front so an unsupported via performs no mutation
	// and never resolves a task.
	switch opts.Via {
	case "pr":
		// supported below
	default:
		return nil, fmt.Errorf("unsupported via mode %q (supported: %s)", opts.Via, shipSupportedModes)
	}

	// Resolve the source repo (no side effects, no lock).
	target, err := ResolveTarget(ctx, client, taskID, "")
	if err != nil {
		return nil, err
	}

	defaultBranch, err := DefaultBranch(ctx, target.SourceRepo)
	if err != nil {
		return nil, fmt.Errorf("determine default branch: %w", err)
	}
	if opts.Branch == defaultBranch {
		return nil, fmt.Errorf("refusing to ship default branch %q", opts.Branch)
	}

	// The branch must be a real local ref before we touch origin.
	if _, err := runGit(ctx, target.SourceRepo, "rev-parse", "--verify", "--quiet", "refs/heads/"+opts.Branch); err != nil {
		return nil, fmt.Errorf("branch %q does not exist locally in %s", opts.Branch, target.SourceRepo)
	}

	// Push under the per-source-repo lock so concurrent git mutations serialize.
	if err := func() error {
		mu := lockSourceRepo(target.SourceRepo)
		defer mu.Unlock()
		if out, err := runGit(ctx, target.SourceRepo, "push", "origin", opts.Branch); err != nil {
			return fmt.Errorf("push %s: %w; log:\n%s", opts.Branch, err, out)
		}
		return nil
	}(); err != nil {
		return nil, err
	}

	// Open the PR. Default the title to the branch's last commit subject. The
	// gh call shells out in the source repo (no local git mutation) so it runs
	// outside the lock, matching iris:gh_pr_create.
	title := strings.TrimSpace(opts.PRTitle)
	if title == "" {
		subj, err := runGit(ctx, target.SourceRepo, "log", "-1", "--format=%s", "refs/heads/"+opts.Branch)
		if err != nil {
			return nil, fmt.Errorf("read last commit subject of %s: %w", opts.Branch, err)
		}
		title = strings.TrimSpace(subj)
	}
	if title == "" {
		return nil, fmt.Errorf("could not derive a PR title for %s (empty commit subject); pass pr_title", opts.Branch)
	}

	pr, err := createPRForBranch(ctx, target.SourceRepo, defaultBranch, opts.Branch, title, opts.PRBody)
	if err != nil {
		return nil, err
	}

	return &ShipFeatureResult{
		Shipped:  true,
		Branch:   opts.Branch,
		PRNumber: pr.Number,
		PRURL:    pr.URL,
		Merged:   false,
		Fetched:  false,
	}, nil
}

// createPRForBranch opens a GitHub PR for head against base in sourceRepo via
// the gh CLI, mirroring iris:gh_pr_create but parameterized on an explicit
// head branch (gh_pr_create operates on the task's own branch).
func createPRForBranch(ctx context.Context, sourceRepo, base, head, title, body string) (*GHPRCreateResult, error) {
	args := []string{
		"pr", "create",
		"--base", base,
		"--head", head,
		"--title", title,
	}
	if body != "" {
		args = append(args, "--body", body)
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = sourceRepo
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh pr create: %w: %s", err, strings.TrimSpace(string(out)))
	}

	url := lastNonEmptyLine(string(out))
	if url == "" {
		return nil, fmt.Errorf("gh pr create returned no URL; output:\n%s", string(out))
	}
	m := ghPRURLRegexp.FindStringSubmatch(url)
	if len(m) != 2 {
		return nil, fmt.Errorf("could not parse PR number from %q (full output:\n%s)", url, string(out))
	}
	num, err := strconv.Atoi(m[1])
	if err != nil {
		return nil, fmt.Errorf("parse PR number %q: %w", m[1], err)
	}
	return &GHPRCreateResult{Number: num, URL: url}, nil
}

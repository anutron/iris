package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// GHPRCreateOptions captures the per-call knobs for GHPRCreate.
type GHPRCreateOptions struct {
	Title string
	Body  string
	Draft bool
	// Head, when non-empty, overrides the task's resolved branch as the PR head.
	// The effective head is this value; the default-branch refusal applies.
	Head string
}

// GHPRCreateResult is the structured success payload.
type GHPRCreateResult struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

var ghPRURLRegexp = regexp.MustCompile(`/pull/(\d+)`)

// GHPRCreate creates a GitHub pull request for the task's branch via the
// gh CLI. Resolves the source repo and branch from task_id, refuses to
// create a PR from the default branch, and shells out to gh in the
// source repo so the host's gh auth and remotes apply.
func GHPRCreate(ctx context.Context, client *argus.Client, taskID string, opts GHPRCreateOptions) (*GHPRCreateResult, error) {
	if strings.TrimSpace(opts.Title) == "" {
		return nil, fmt.Errorf("title is required")
	}

	resolved, err := Resolve(ctx, client, taskID)
	if err != nil {
		return nil, err
	}

	defaultBranch, err := DefaultBranch(ctx, resolved.SourceRepo)
	if err != nil {
		return nil, fmt.Errorf("determine default branch: %w", err)
	}

	// Compute the effective head: use the override when provided, otherwise
	// fall back to the task's resolved branch.
	effective := resolved.Branch
	if opts.Head != "" {
		effective = opts.Head
	}

	if effective == "" {
		return nil, fmt.Errorf("task has no current branch")
	}
	if effective == defaultBranch {
		return nil, fmt.Errorf("refusing to open PR from default branch %q", effective)
	}

	args := []string{"pr", "create", "--title", opts.Title}
	if fu := detectForkUpstream(ctx, resolved.SourceRepo); fu != nil {
		// Cross-fork: origin is a fork, so target the upstream parent and
		// qualify the effective head with the fork owner. Omit --base; gh
		// defaults it to the upstream repository's default branch.
		args = append(args, "--repo", fu.UpstreamRepo, "--head", fu.ForkOwner+":"+effective)
	} else {
		// Same-repo: origin is the target.
		args = append(args, "--base", defaultBranch, "--head", effective)
	}
	if opts.Body != "" {
		args = append(args, "--body", opts.Body)
	}
	if opts.Draft {
		args = append(args, "--draft")
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = resolved.SourceRepo
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

// forkUpstream describes a fork → upstream relationship discovered for the
// source repo's origin.
type forkUpstream struct {
	ForkOwner    string // owner of origin (the fork), e.g. "anutron"
	UpstreamRepo string // "<owner>/<repo>" of the upstream parent, e.g. "drn/argus"
}

// detectForkUpstream best-effort inspects the source repo's origin via gh and
// reports the fork → upstream relationship when origin is a GitHub fork.
//
// It returns nil — and callers fall back to a same-repo PR — when origin is not
// a fork OR when the relationship cannot be determined (gh unavailable, offline,
// unexpected JSON). Detection must never break the common same-repo case, so
// every failure path is a silent nil rather than an error.
func detectForkUpstream(ctx context.Context, sourceRepo string) *forkUpstream {
	cmd := exec.CommandContext(ctx, "gh", "repo", "view", "--json", "nameWithOwner,parent")
	cmd.Dir = sourceRepo
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var v struct {
		NameWithOwner string `json:"nameWithOwner"`
		Parent        *struct {
			Name  string `json:"name"`
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"parent"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil
	}
	if v.Parent == nil || v.Parent.Name == "" || v.Parent.Owner.Login == "" {
		return nil // origin is not a fork
	}
	forkOwner := v.NameWithOwner
	if i := strings.IndexByte(forkOwner, '/'); i >= 0 {
		forkOwner = forkOwner[:i]
	}
	if forkOwner == "" {
		return nil
	}
	return &forkUpstream{
		ForkOwner:    forkOwner,
		UpstreamRepo: v.Parent.Owner.Login + "/" + v.Parent.Name,
	}
}

// lastNonEmptyLine returns the trailing non-empty line of s with surrounding
// whitespace trimmed. gh prints the PR URL last on success; empty lines or
// trailing newlines are common.
func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

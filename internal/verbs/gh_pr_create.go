package verbs

import (
	"context"
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

	if resolved.Branch == "" {
		return nil, fmt.Errorf("task has no current branch")
	}
	if resolved.Branch == defaultBranch {
		return nil, fmt.Errorf("refusing to open PR from default branch %q", resolved.Branch)
	}

	args := []string{
		"pr", "create",
		"--base", defaultBranch,
		"--head", resolved.Branch,
		"--title", opts.Title,
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

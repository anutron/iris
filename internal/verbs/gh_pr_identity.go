package verbs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// prIdentity is what gh reports about a PR before iris acts on it. Repo is
// parsed from URL, so it names the repo gh itself resolved (which can be an
// upstream remote or a `gh repo set-default` target, not necessarily origin).
type prIdentity struct {
	URL   string
	Title string
	Repo  string
}

// lookupPR runs `gh pr view <n> --json url,title` in sourceRepo. Only
// stdout is parsed: gh writes notices (upgrade prompts, deprecations) to
// stderr, and mixing them in would turn a matching repo into a refusal.
func lookupPR(ctx context.Context, sourceRepo string, prNumber int) (*prIdentity, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", fmt.Sprintf("%d", prNumber), "--json", "url,title")
	cmd.Dir = sourceRepo
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr view: %w: %s", err, strings.TrimSpace(stderr.String()+string(out)))
	}
	var v struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("gh pr view: unexpected output: %s", strings.TrimSpace(string(out)))
	}
	repo := repoFromPRURL(v.URL)
	if repo == "" {
		return nil, fmt.Errorf("gh pr view: no PR URL in output: %s", strings.TrimSpace(string(out)))
	}
	return &prIdentity{URL: v.URL, Title: v.Title, Repo: repo}, nil
}

// identifyPR looks the PR up and applies the optional expected_repo guard.
// With no guard, a failed lookup is returned as a non-fatal warning so the
// verb behaves as it did before the lookup existed. With a guard, any
// failure to confirm the repo is fatal: an irreversible action must not
// proceed on an unconfirmed target.
func identifyPR(ctx context.Context, sourceRepo string, prNumber int, expectedRepo string) (id *prIdentity, warning string, err error) {
	id, lookupErr := lookupPR(ctx, sourceRepo, prNumber)
	if lookupErr != nil {
		if expectedRepo != "" {
			return nil, "", fmt.Errorf("refusing: could not confirm PR #%d belongs to expected_repo %s: %v", prNumber, expectedRepo, lookupErr)
		}
		return nil, fmt.Sprintf("could not identify PR #%d before acting: %v", prNumber, lookupErr), nil
	}
	if expectedRepo != "" && !strings.EqualFold(id.Repo, expectedRepo) {
		return nil, "", fmt.Errorf("refusing: PR #%d is %s (%q) but expected_repo is %s; nothing was changed. The task_id resolves to a different repo than you expected",
			prNumber, id.URL, id.Title, expectedRepo)
	}
	return id, "", nil
}

// repoArgs binds a gh pr action to the repo the lookup verified, so gh
// can't resolve a different one on its own between the check and the act.
// Without an identity (lookup failed, no guard) gh resolves as it always has.
func repoArgs(id *prIdentity) []string {
	if id == nil {
		return nil
	}
	return []string{"--repo", id.Repo}
}

// validateExpectedRepo checks the optional expected_repo shape.
func validateExpectedRepo(expectedRepo string) error {
	if expectedRepo != "" && !ValidExpectedRepo(expectedRepo) {
		return fmt.Errorf("expected_repo %q must have the form owner/name", expectedRepo)
	}
	return nil
}

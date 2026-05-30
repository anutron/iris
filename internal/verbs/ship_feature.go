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
// `pr` stops after opening the PR. `pr-auto` continues: it waits for the PR's
// required CI checks to pass, then approves, merges (using merge_method), and
// fetches origin. The post-ship dogfood re-compose is a later stage; pr-auto
// leaves the Recompose field nil for now. An unknown/unsupported via is refused
// naming the modes iris currently supports.
//
// See openspec/changes/add-dogfood-and-ship-verbs/design.md
// ("Ship is an orchestrator, not a primitive" + "Two via modes" + the
// "[Risk] pr-auto merges a PR with failing CI" mitigation) and
// specs/iris-ship-feature/spec.md.

package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
)

// ShipFeatureOpts captures the typed arguments for ShipFeature.
type ShipFeatureOpts struct {
	// Branch is the local feature branch to ship. MUST exist locally and MUST
	// NOT be the source repo's default branch.
	Branch string
	// Via selects the ship mode: "pr" (push + open PR, stop) or "pr-auto"
	// (push + open + wait-for-CI + approve + merge + fetch).
	Via string
	// PRTitle is the PR title; defaults to the branch's last commit subject
	// when empty.
	PRTitle string
	// PRBody is the optional PR body.
	PRBody string
	// MergeMethod is "squash" (default), "merge", or "rebase". Unused in pr
	// mode; selects the merge strategy in pr-auto.
	MergeMethod string
}

// Warning is a structured, non-fatal warning. pr mode emits none; pr-auto
// uses these to surface ci_failed / ci_timeout outcomes.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RecomposeResult describes the post-ship dogfood re-compose outcome. It is
// populated by the Stage 7 re-compose; pr-auto leaves it nil for now.
type RecomposeResult struct {
	Attempted bool               `json:"attempted"`
	Succeeded bool               `json:"succeeded"`
	NewSHA    string             `json:"new_sha,omitempty"`
	Conflict  *RecomposeConflict `json:"conflict,omitempty"`
}

// RecomposeConflict names the layered branch that failed to re-apply cleanly
// during a post-ship re-compose, leaving the dogfood state untouched.
type RecomposeConflict struct {
	Branch  string `json:"branch"`
	Message string `json:"message"`
}

// ShipFeatureResult is the structured success payload, mirrored to MCP/CLI as
// pretty-printed JSON.
type ShipFeatureResult struct {
	Shipped  bool      `json:"shipped"`
	Branch   string    `json:"branch"`
	PRNumber int       `json:"pr_number"`
	PRURL    string    `json:"pr_url"`
	Merged    bool             `json:"merged"` // always false in pr mode
	MergeSHA  string           `json:"merge_sha,omitempty"`
	Fetched   bool             `json:"fetched"` // always false in pr mode
	Recompose *RecomposeResult `json:"recompose,omitempty"`
	Warnings  []Warning        `json:"warnings,omitempty"`
}

// shipSupportedModes is the human-readable list of via modes iris implements.
const shipSupportedModes = `"pr" or "pr-auto"`

// shipCheckPollInterval is how often pr-auto re-queries the PR's CI check
// status while waiting. A package var so tests can shrink it to avoid real
// sleeps; production leaves it at the 5s default.
var shipCheckPollInterval = 5 * time.Second

// shipCITimeoutOverride, when non-nil, replaces the .iris.toml-derived CI wait
// timeout. Tests set it (e.g. 200ms) so the timeout scenario stays fast;
// production leaves it nil and the config value (default 600s) governs.
var shipCITimeoutOverride *time.Duration

// ShipFeature ships opts.Branch to origin's default branch via a GitHub PR.
//
// pr mode sequence: validate via -> resolve source repo -> reject default
// branch -> require branch to exist locally -> push branch to origin -> open a
// PR targeting the default branch. It never merges, fetches, or touches the
// dogfood branch/manifest.
//
// pr-auto mode continues past the open PR: wait for the head commit's required
// CI checks -> on pass, approve -> merge (merge_method) -> fetch origin. If CI
// fails or times out, it leaves the PR open and returns shipped=false with a
// ci_failed / ci_timeout warning, never approving or merging. The post-ship
// dogfood re-compose (Recompose) is Stage 7 and stays nil here.
//
// All refusals happen before any mutation: an unknown via (and a bad pr-auto
// merge_method) is rejected before resolution, and the default-branch /
// missing-branch refusals are checked before the push.
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
	case "pr-auto":
		// A bad merge_method must refuse before any push, not after.
		if opts.MergeMethod != "" && !IsValidGHPRMergeStrategy(opts.MergeMethod) {
			return nil, fmt.Errorf("invalid merge_method %q (must be one of squash|merge|rebase)", opts.MergeMethod)
		}
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

	// pr mode stops here: no merge, no fetch, no dogfood mutation.
	if opts.Via == "pr" {
		return &ShipFeatureResult{
			Shipped:  true,
			Branch:   opts.Branch,
			PRNumber: pr.Number,
			PRURL:    pr.URL,
			Merged:   false,
			Fetched:  false,
		}, nil
	}

	// pr-auto: wait for CI -> approve -> merge -> fetch.
	//
	// The pushed branch tip is the PR's head commit, so its local SHA is the
	// commit whose check-runs we poll.
	headSHA, err := runGit(ctx, target.SourceRepo, "rev-parse", "refs/heads/"+opts.Branch)
	if err != nil {
		return nil, fmt.Errorf("resolve head SHA of %s: %w", opts.Branch, err)
	}
	headSHA = strings.TrimSpace(headSHA)

	timeout := shipCITimeout(target.SourceRepo)
	passed, warning, err := waitForChecks(ctx, target.SourceRepo, pr.Number, headSHA, timeout)
	if err != nil {
		return nil, err
	}
	if !passed {
		// CI failed or timed out. Leave the PR open; do NOT approve or merge.
		return &ShipFeatureResult{
			Shipped:  false,
			Branch:   opts.Branch,
			PRNumber: pr.Number,
			PRURL:    pr.URL,
			Merged:   false,
			Warnings: []Warning{*warning},
		}, nil
	}

	method := opts.MergeMethod
	if method == "" {
		method = "squash"
	}

	if err := approvePRForShip(ctx, target.SourceRepo, pr.Number); err != nil {
		return nil, err
	}
	if err := mergePRForShip(ctx, target.SourceRepo, pr.Number, method); err != nil {
		return nil, err
	}
	mergeSHA, err := readMergeCommitSHA(ctx, target.SourceRepo, pr.Number)
	if err != nil {
		return nil, err
	}

	// Fetch origin so local tracking refs reflect the just-merged default
	// branch. Fetch acquires the per-source-repo lock itself.
	fetched := false
	if fr, err := Fetch(ctx, FetchInput{Client: client, TaskID: taskID}); err != nil {
		return nil, fmt.Errorf("fetch after merge: %w", err)
	} else {
		fetched = fr.Fetched
	}

	// Recompose is left nil here: the post-ship dogfood re-compose is Stage 7.
	return &ShipFeatureResult{
		Shipped:   true,
		Branch:    opts.Branch,
		PRNumber:  pr.Number,
		PRURL:     pr.URL,
		Merged:    true,
		MergeSHA:  mergeSHA,
		Fetched:   fetched,
		Recompose: nil,
	}, nil
}

// shipCITimeout resolves how long pr-auto waits for CI checks. It reads
// ship_ci_timeout_seconds from the source repo's .iris.toml (default 600s),
// tolerating a missing or invalid config by falling back to the default. A
// non-nil shipCITimeoutOverride wins, so tests can drive a sub-second timeout.
func shipCITimeout(sourceRepo string) time.Duration {
	timeout := time.Duration(config.DefaultShipCITimeoutSeconds) * time.Second
	tomlPath := filepath.Join(sourceRepo, config.IrisTomlFilename)
	if doc, _, _ := config.LoadIrisToml(tomlPath, false); doc != nil {
		timeout = time.Duration(doc.ResolvedShipCITimeoutSeconds()) * time.Second
	}
	if shipCITimeoutOverride != nil {
		timeout = *shipCITimeoutOverride
	}
	return timeout
}

// checkRun is the subset of a GitHub check-run iris inspects.
type checkRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`     // queued | in_progress | completed | ...
	Conclusion string `json:"conclusion"` // success | failure | ... (when completed)
}

// waitForChecks polls the head commit's check-runs until they all conclude
// successfully, any conclude in failure, or the timeout elapses.
//
// Returns (true, nil, nil) when checks pass OR when GitHub reports zero checks
// for the head SHA (nothing to wait on). Returns (false, &Warning{ci_failed},
// nil) on the first failing check and (false, &Warning{ci_timeout}, nil) when
// pending checks outlast the timeout. The caller does NOT approve or merge
// unless passed is true. Extracted so Stage 7 and tests can target it cleanly.
func waitForChecks(ctx context.Context, sourceRepo string, prNumber int, headSHA string, timeout time.Duration) (bool, *Warning, error) {
	deadline := time.Now().Add(timeout)
	for {
		runs, err := queryCheckRuns(ctx, sourceRepo, headSHA)
		if err != nil {
			return false, nil, err
		}
		if len(runs) == 0 {
			// Zero required checks reported: proceed immediately.
			return true, nil, nil
		}
		failing, pending := summarizeCheckRuns(runs)
		if len(failing) > 0 {
			return false, &Warning{
				Code:    "ci_failed",
				Message: fmt.Sprintf("required check(s) failed on %s: %s", headSHA, strings.Join(failing, ", ")),
			}, nil
		}
		if len(pending) == 0 {
			return true, nil, nil
		}
		if !time.Now().Before(deadline) {
			return false, &Warning{
				Code:    "ci_timeout",
				Message: fmt.Sprintf("timed out after %s waiting for check(s): %s", timeout, strings.Join(pending, ", ")),
			}, nil
		}
		select {
		case <-ctx.Done():
			return false, nil, ctx.Err()
		case <-time.After(shipCheckPollInterval):
		}
	}
}

// queryCheckRuns reads the check-runs for headSHA via the host gh CLI's API
// passthrough. gh substitutes {owner}/{repo} from the source repo context.
func queryCheckRuns(ctx context.Context, sourceRepo, headSHA string) ([]checkRun, error) {
	path := fmt.Sprintf("repos/{owner}/{repo}/commits/%s/check-runs", headSHA)
	cmd := exec.CommandContext(ctx, "gh", "api", path)
	cmd.Dir = sourceRepo
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh api check-runs: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var resp struct {
		TotalCount int        `json:"total_count"`
		CheckRuns  []checkRun `json:"check_runs"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parse check-runs JSON: %w; output:\n%s", err, string(out))
	}
	return resp.CheckRuns, nil
}

// summarizeCheckRuns partitions check-runs into failing and still-pending by
// name. A run that has not completed is pending; a completed run counts as
// passing only when its conclusion is success, neutral, or skipped.
func summarizeCheckRuns(runs []checkRun) (failing, pending []string) {
	for _, r := range runs {
		if r.Status != "completed" {
			pending = append(pending, r.Name)
			continue
		}
		switch r.Conclusion {
		case "success", "neutral", "skipped":
			// passing
		default:
			failing = append(failing, r.Name)
		}
	}
	return failing, pending
}

// approvePRForShip approves the PR via the host gh CLI.
func approvePRForShip(ctx context.Context, sourceRepo string, prNumber int) error {
	cmd := exec.CommandContext(ctx, "gh", "pr", "review", strconv.Itoa(prNumber), "--approve")
	cmd.Dir = sourceRepo
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh pr review --approve: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// mergePRForShip merges the PR with the given strategy via the host gh CLI.
func mergePRForShip(ctx context.Context, sourceRepo string, prNumber int, method string) error {
	cmd := exec.CommandContext(ctx, "gh", "pr", "merge", "--"+method, strconv.Itoa(prNumber))
	cmd.Dir = sourceRepo
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh pr merge --%s: %w: %s", method, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// readMergeCommitSHA reads the merged PR's merge commit SHA via the host gh
// CLI. Returns "" (no error) when GitHub has not yet recorded a merge commit.
func readMergeCommitSHA(ctx context.Context, sourceRepo string, prNumber int) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", strconv.Itoa(prNumber), "--json", "mergeCommit")
	cmd.Dir = sourceRepo
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh pr view --json mergeCommit: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var resp struct {
		MergeCommit struct {
			OID string `json:"oid"`
		} `json:"mergeCommit"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", fmt.Errorf("parse mergeCommit JSON: %w; output:\n%s", err, string(out))
	}
	return resp.MergeCommit.OID, nil
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

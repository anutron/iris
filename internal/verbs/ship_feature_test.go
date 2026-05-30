package verbs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/anutron/iris/internal/argus"
)

// setupShipRepo builds a bare origin + source clone + argus worktree (via the
// shared push fixture), then creates a feature branch in the source repo with
// one commit ahead of main. Returns (src, wt, bare, featureBranch, featureSHA,
// client). The feature branch is a real local ref in the source repo, distinct
// from both the default branch and the worktree's argus branch — i.e. exactly
// the shape ship_feature ships.
func setupShipRepo(t *testing.T, slug, feature string) (src, wt, bare, featureSHA string, client *argus.Client) {
	t.Helper()
	src, wt, bare = setupRepoWithBareAndWorktree(t, slug)
	g := gitRunner(t)
	g(src, "checkout", "-b", feature, "main")
	g(src, "commit", "--allow-empty", "-m", "feature work: "+feature)
	featureSHA = revParse(t, src, "refs/heads/"+feature)
	client = stubArgus(t, src, wt)
	return
}

// fakeGHPRCreateOnly is a fake gh that captures argv and emits a PR URL,
// mimicking a successful `gh pr create`.
const fakeGHPRCreateOnly = fakeGHCaptureArgv + `
echo "Creating pull request..."
echo ""
echo "https://github.com/anutron/iris/pull/77"
exit 0
`

// check-runs API response fixtures used by the pr-auto tests.
const (
	checksAllPass = `{"total_count":2,"check_runs":[{"name":"build","status":"completed","conclusion":"success"},{"name":"test","status":"completed","conclusion":"success"}]}`
	checksOneFail = `{"total_count":2,"check_runs":[{"name":"build","status":"completed","conclusion":"success"},{"name":"test","status":"completed","conclusion":"failure"}]}`
	checksPending = `{"total_count":1,"check_runs":[{"name":"test","status":"in_progress","conclusion":null}]}`
	checksNone    = `{"total_count":0,"check_runs":[]}`
)

// shrinkShipPolling shrinks the pr-auto poll interval to 1ms and pins the CI
// wait timeout to the given duration for the remainder of the test, restoring
// both afterward. Keeps the timeout scenario fast and deterministic.
func shrinkShipPolling(t *testing.T, timeout time.Duration) {
	t.Helper()
	prevInterval := shipCheckPollInterval
	prevOverride := shipCITimeoutOverride
	shipCheckPollInterval = time.Millisecond
	to := timeout
	shipCITimeoutOverride = &to
	t.Cleanup(func() {
		shipCheckPollInterval = prevInterval
		shipCITimeoutOverride = prevOverride
	})
}

func TestShipFeature_PRAutoHappyPath(t *testing.T) {
	_, _, _, _, client := setupShipRepo(t, "ship-auto-happy", "feature/F2")
	dir := writeFakeGH(t, fakeGHPRAutoBody(checksAllPass))

	result, err := ShipFeature(context.Background(), client, "task-ship", ShipFeatureOpts{
		Branch: "feature/F2",
		Via:    "pr-auto",
	})
	if err != nil {
		t.Fatalf("ShipFeature: %v", err)
	}
	if !result.Shipped {
		t.Fatal("expected Shipped=true")
	}
	if !result.Merged {
		t.Fatal("expected Merged=true")
	}
	if !result.Fetched {
		t.Fatal("expected Fetched=true")
	}
	if result.MergeSHA == "" {
		t.Fatal("expected non-empty MergeSHA")
	}
	if result.Recompose != nil {
		t.Fatalf("Stage 6 must leave Recompose nil; got %+v", result.Recompose)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("happy path must have no warnings; got %+v", result.Warnings)
	}
	// The full sequence ran: PR create, check-runs poll, approve, merge, view.
	calls := readFakeGHCalls(t, dir)
	for _, want := range []string{"pr create", "api repos", "check-runs", "--approve", "pr merge --squash", "pr view"} {
		if !strings.Contains(calls, want) {
			t.Fatalf("expected gh calls to include %q; calls:\n%s", want, calls)
		}
	}
}

func TestShipFeature_PRAutoCIFail(t *testing.T) {
	_, _, _, _, client := setupShipRepo(t, "ship-auto-cifail", "feature/F2")
	dir := writeFakeGH(t, fakeGHPRAutoBody(checksOneFail))

	result, err := ShipFeature(context.Background(), client, "task-ship", ShipFeatureOpts{
		Branch: "feature/F2",
		Via:    "pr-auto",
	})
	if err != nil {
		t.Fatalf("ShipFeature returned a hard error; CI failure should be a warning: %v", err)
	}
	if result.Shipped {
		t.Fatal("CI failure must leave Shipped=false")
	}
	if result.Merged {
		t.Fatal("CI failure must leave Merged=false")
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Code != "ci_failed" {
		t.Fatalf("expected a single ci_failed warning; got %+v", result.Warnings)
	}
	// The PR is still addressable so the agent can revisit it.
	if result.PRNumber == 0 || result.PRURL == "" {
		t.Fatalf("expected PR identity preserved on CI failure; got #%d %q", result.PRNumber, result.PRURL)
	}
	// No approve, no merge.
	calls := readFakeGHCalls(t, dir)
	if strings.Contains(calls, "review") {
		t.Fatalf("CI failure must NOT approve the PR; calls:\n%s", calls)
	}
	if strings.Contains(calls, "merge") {
		t.Fatalf("CI failure must NOT merge the PR; calls:\n%s", calls)
	}
}

func TestShipFeature_PRAutoCITimeout(t *testing.T) {
	_, _, _, _, client := setupShipRepo(t, "ship-auto-timeout", "feature/F2")
	dir := writeFakeGH(t, fakeGHPRAutoBody(checksPending))
	shrinkShipPolling(t, 200*time.Millisecond)

	result, err := ShipFeature(context.Background(), client, "task-ship", ShipFeatureOpts{
		Branch: "feature/F2",
		Via:    "pr-auto",
	})
	if err != nil {
		t.Fatalf("ShipFeature returned a hard error; a CI timeout should be a warning: %v", err)
	}
	if result.Shipped {
		t.Fatal("CI timeout must leave Shipped=false")
	}
	if result.Merged {
		t.Fatal("CI timeout must leave Merged=false")
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Code != "ci_timeout" {
		t.Fatalf("expected a single ci_timeout warning; got %+v", result.Warnings)
	}
	calls := readFakeGHCalls(t, dir)
	if strings.Contains(calls, "merge") {
		t.Fatalf("CI timeout must NOT merge the PR; calls:\n%s", calls)
	}
	if strings.Contains(calls, "review") {
		t.Fatalf("CI timeout must NOT approve the PR; calls:\n%s", calls)
	}
}

func TestShipFeature_PRAutoNoChecks(t *testing.T) {
	_, _, _, _, client := setupShipRepo(t, "ship-auto-nochecks", "feature/F2")
	dir := writeFakeGH(t, fakeGHPRAutoBody(checksNone))
	// A tiny poll interval guards against accidental waiting; with zero checks
	// the wait step should be skipped entirely regardless.
	shrinkShipPolling(t, 50*time.Millisecond)

	result, err := ShipFeature(context.Background(), client, "task-ship", ShipFeatureOpts{
		Branch:      "feature/F2",
		Via:         "pr-auto",
		MergeMethod: "merge",
	})
	if err != nil {
		t.Fatalf("ShipFeature: %v", err)
	}
	if !result.Shipped || !result.Merged {
		t.Fatalf("zero checks must proceed to merge: shipped=%v merged=%v", result.Shipped, result.Merged)
	}
	// Merge proceeded with the requested method.
	calls := readFakeGHCalls(t, dir)
	if !strings.Contains(calls, "pr merge --merge") {
		t.Fatalf("expected merge with --merge method; calls:\n%s", calls)
	}
}

func TestShipFeature_PRModePushesAndOpensPR(t *testing.T) {
	_, _, bare, featureSHA, client := setupShipRepo(t, "ship-pr-happy", "feature/F2")
	dir := writeFakeGH(t, fakeGHPRCreateOnly)

	result, err := ShipFeature(context.Background(), client, "task-ship", ShipFeatureOpts{
		Branch: "feature/F2",
		Via:    "pr",
	})
	if err != nil {
		t.Fatalf("ShipFeature: %v", err)
	}
	if !result.Shipped {
		t.Fatal("expected Shipped=true")
	}
	if result.Branch != "feature/F2" {
		t.Fatalf("Branch: got %q want feature/F2", result.Branch)
	}
	if result.Merged {
		t.Fatal("pr mode must not merge (Merged should be false)")
	}
	if result.Fetched {
		t.Fatal("pr mode must not fetch (Fetched should be false)")
	}
	if result.PRNumber != 77 {
		t.Fatalf("PRNumber: got %d want 77", result.PRNumber)
	}
	if result.PRURL == "" {
		t.Fatal("PRURL should be non-empty")
	}
	// The branch actually reached origin.
	if got := remoteRef(t, bare, "feature/F2"); got != featureSHA {
		t.Fatalf("origin feature/F2: got %q want %q", got, featureSHA)
	}
	// gh was invoked for `pr create` targeting main.
	argv := readFakeGHArgv(t, dir)
	if !strings.Contains(argv, "pr") || !strings.Contains(argv, "create") {
		t.Fatalf("expected gh pr create invocation; argv:\n%s", argv)
	}
	if !strings.Contains(argv, "feature/F2") {
		t.Fatalf("expected --head feature/F2 in argv:\n%s", argv)
	}
}

func TestShipFeature_PRModeDoesNotMergeOrFetch(t *testing.T) {
	src, _, _, _, client := setupShipRepo(t, "ship-pr-nomerge", "feature/F2")
	dir := writeFakeGH(t, fakeGHPRCreateOnly)

	result, err := ShipFeature(context.Background(), client, "task-ship", ShipFeatureOpts{
		Branch: "feature/F2",
		Via:    "pr",
	})
	if err != nil {
		t.Fatalf("ShipFeature: %v", err)
	}
	if result.Merged || result.Fetched {
		t.Fatalf("pr mode must not merge/fetch: merged=%v fetched=%v", result.Merged, result.Fetched)
	}
	// The only gh call was `pr create` — never `pr merge`.
	argv := readFakeGHArgv(t, dir)
	if strings.Contains(argv, "merge") {
		t.Fatalf("pr mode must not call the merge API; argv:\n%s", argv)
	}
	// No dogfood manifest was written or touched.
	stateDir, _ := SourceRepoStateDir(canonicalize(src))
	if m, _ := ReadManifest(stateDir); m != nil {
		t.Fatal("pr mode must not write a dogfood manifest")
	}
}

func TestShipFeature_RefusesDefaultBranch(t *testing.T) {
	_, _, bare, _, client := setupShipRepo(t, "ship-default", "feature/F2")
	dir := writeFakeGH(t, fakeGHPRCreateOnly)
	before := remoteRef(t, bare, "main")

	_, err := ShipFeature(context.Background(), client, "task-ship", ShipFeatureOpts{
		Branch: "main",
		Via:    "pr",
	})
	if err == nil {
		t.Fatal("expected refusal to ship the default branch")
	}
	if !strings.Contains(err.Error(), "refusing to ship default branch") {
		t.Fatalf("error should name the refusal: %v", err)
	}
	if argv := readFakeGHArgv(t, dir); argv != "" {
		t.Fatalf("gh must NOT be invoked on refusal; argv:\n%s", argv)
	}
	if after := remoteRef(t, bare, "main"); after != before {
		t.Fatalf("origin/main changed on refusal: before=%s after=%s", before, after)
	}
}

func TestShipFeature_RefusesUnknownViaMode(t *testing.T) {
	_, _, _, _, client := setupShipRepo(t, "ship-badvia", "feature/F2")
	dir := writeFakeGH(t, fakeGHPRCreateOnly)

	_, err := ShipFeature(context.Background(), client, "task-ship", ShipFeatureOpts{
		Branch: "feature/F2",
		Via:    "anything-else",
	})
	if err == nil {
		t.Fatal("expected refusal for unknown via mode")
	}
	// Error names the supported mode(s).
	if !strings.Contains(err.Error(), "pr") {
		t.Fatalf("error should name supported modes: %v", err)
	}
	if argv := readFakeGHArgv(t, dir); argv != "" {
		t.Fatalf("gh must NOT be invoked on unknown via; argv:\n%s", argv)
	}
}

func TestShipFeature_RefusesMissingBranch(t *testing.T) {
	_, _, bare, _, client := setupShipRepo(t, "ship-missing", "feature/F2")
	dir := writeFakeGH(t, fakeGHPRCreateOnly)

	_, err := ShipFeature(context.Background(), client, "task-ship", ShipFeatureOpts{
		Branch: "feature/does-not-exist",
		Via:    "pr",
	})
	if err == nil {
		t.Fatal("expected refusal for a branch that does not exist locally")
	}
	if argv := readFakeGHArgv(t, dir); argv != "" {
		t.Fatalf("gh must NOT be invoked when the branch is missing; argv:\n%s", argv)
	}
	// Nothing was pushed.
	if got := remoteRef(t, bare, "feature/does-not-exist"); got != "" {
		t.Fatalf("missing branch must not reach origin; got %q", got)
	}
}

// TestShipFeature_ResultMarshalsAsJSON covers the "Direct CLI invocation mirrors
// MCP" scenario at the data layer: the same verbs.ShipFeature result serializes
// to the documented pretty-printed JSON shape both the CLI and MCP handler print.
func TestShipFeature_ResultMarshalsAsJSON(t *testing.T) {
	_, _, _, _, client := setupShipRepo(t, "ship-json", "feature/F2")
	writeFakeGH(t, fakeGHPRCreateOnly)

	result, err := ShipFeature(context.Background(), client, "task-ship", ShipFeatureOpts{
		Branch: "feature/F2",
		Via:    "pr",
	})
	if err != nil {
		t.Fatalf("ShipFeature: %v", err)
	}
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	for _, key := range []string{`"shipped"`, `"branch"`, `"pr_number"`, `"pr_url"`, `"merged"`, `"fetched"`} {
		if !strings.Contains(string(body), key) {
			t.Fatalf("result JSON missing %s:\n%s", key, body)
		}
	}
}

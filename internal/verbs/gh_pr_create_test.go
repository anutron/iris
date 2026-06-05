package verbs

import (
	"context"
	"strings"
	"testing"
)

const fakeGHCaptureArgv = `
{
  for arg in "$@"; do
    printf '%s\n' "$arg"
  done
} > "$IRIS_FAKE_GH_DIR/argv"
`

func TestGHPRCreate_HappyPath(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-happy")
	client := stubArgus(t, src, wt)

	body := fakeGHCaptureArgv + `
echo "Creating pull request for argus/ghpr-happy into main in anutron/iris"
echo ""
echo "https://github.com/anutron/iris/pull/42"
exit 0
`
	dir := writeFakeGH(t, body)

	result, err := GHPRCreate(context.Background(), client, "task-ghpr-happy", GHPRCreateOptions{Title: "Add a thing"})
	if err != nil {
		t.Fatalf("gh pr create: %v", err)
	}
	if result.Number != 42 {
		t.Fatalf("unexpected PR number: got %d, want 42", result.Number)
	}
	if result.URL != "https://github.com/anutron/iris/pull/42" {
		t.Fatalf("unexpected URL: %q", result.URL)
	}
	argv := readFakeGHArgv(t, dir)
	if argv == "" {
		t.Fatal("expected gh to be invoked")
	}
	if !strings.Contains(argv, "--title") || !strings.Contains(argv, "Add a thing") {
		t.Fatalf("argv missing title flag/value:\n%s", argv)
	}
}

func TestGHPRCreate_RefusesDefaultBranch(t *testing.T) {
	tmp := t.TempDir()
	g := gitRunner(t)
	bare := tmp + "/origin.git"
	src := tmp + "/src"
	wt := tmp + "/wt"

	g("", "init", "--bare", "-b", "main", bare)
	g("", "clone", bare, src)
	g(src, "config", "user.email", "x@y.z")
	g(src, "config", "user.name", "x")
	g(src, "commit", "--allow-empty", "-m", "initial")
	g(src, "push", "-u", "origin", "main")
	g(src, "remote", "set-head", "origin", "main")
	g(src, "switch", "-c", "iris-test-host")
	g(src, "worktree", "add", wt, "main")

	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHCaptureArgv+"\nexit 0\n")

	_, err := GHPRCreate(context.Background(), client, "task-default", GHPRCreateOptions{Title: "x"})
	if err == nil {
		t.Fatal("expected refusal for default branch, got nil")
	}
	if !strings.Contains(err.Error(), "default branch") {
		t.Fatalf("unexpected error: %v", err)
	}
	if argv := readFakeGHArgv(t, dir); argv != "" {
		t.Fatalf("expected gh NOT to be invoked, but argv was captured:\n%s", argv)
	}
}

func TestGHPRCreate_DraftFlagRespected(t *testing.T) {
	cases := []struct {
		name        string
		slug        string
		draft       bool
		wantPresent bool
	}{
		{"draft-true-emits-flag", "ghpr-draft-on", true, true},
		{"draft-false-omits-flag", "ghpr-draft-off", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, wt, _ := setupRepoWithBareAndWorktree(t, tc.slug)
			client := stubArgus(t, src, wt)
			body := fakeGHCaptureArgv + `
echo "https://github.com/anutron/iris/pull/7"
exit 0
`
			dir := writeFakeGH(t, body)

			_, err := GHPRCreate(context.Background(), client, "task-d", GHPRCreateOptions{Title: "t", Draft: tc.draft})
			if err != nil {
				t.Fatalf("gh pr create: %v", err)
			}
			argv := readFakeGHArgv(t, dir)
			hasDraft := strings.Contains(argv, "--draft")
			if hasDraft != tc.wantPresent {
				t.Fatalf("--draft present=%v, want %v; argv:\n%s", hasDraft, tc.wantPresent, argv)
			}
		})
	}
}

func TestGHPRCreate_EmptyBodyOmitsFlag(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-nobody")
	client := stubArgus(t, src, wt)
	body := fakeGHCaptureArgv + `
echo "https://github.com/anutron/iris/pull/9"
exit 0
`
	dir := writeFakeGH(t, body)

	_, err := GHPRCreate(context.Background(), client, "task-b", GHPRCreateOptions{Title: "title-only"})
	if err != nil {
		t.Fatalf("gh pr create: %v", err)
	}
	argv := readFakeGHArgv(t, dir)
	if strings.Contains(argv, "--body") {
		t.Fatalf("expected --body to be omitted when body is empty; argv:\n%s", argv)
	}
}

func TestGHPRCreate_NotAuthedSurfacesActionable(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-noauth")
	client := stubArgus(t, src, wt)
	body := fakeGHCaptureArgv + `
echo "error: To get started with GitHub CLI, please run: gh auth login" 1>&2
exit 1
`
	writeFakeGH(t, body)

	_, err := GHPRCreate(context.Background(), client, "task-na", GHPRCreateOptions{Title: "t"})
	if err == nil {
		t.Fatal("expected error from unauthed gh, got nil")
	}
	if !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("expected error to surface gh stderr containing 'gh auth login', got: %v", err)
	}
}

func TestGHPRCreate_RefusesUnknownTaskID(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t)
	_, err := GHPRCreate(context.Background(), client, "ghost-task", GHPRCreateOptions{Title: "t"})
	if err == nil {
		t.Fatal("expected error for unknown task, got nil")
	}
	if !strings.Contains(err.Error(), "ghost-task") {
		t.Fatalf("expected error to name task id, got: %v", err)
	}
}

func TestGHPRCreate_TitleRequired(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t) // never reached
	_, err := GHPRCreate(context.Background(), client, "task-x", GHPRCreateOptions{})
	if err == nil {
		t.Fatal("expected error for missing title, got nil")
	}
	if !strings.Contains(err.Error(), "title") {
		t.Fatalf("expected title-required error, got: %v", err)
	}
}

// TestGHPRCreate_HeadOverride verifies that when opts.Head is non-empty the verb
// passes the override branch as --head (same-repo form) and NOT the resolved
// task branch.
func TestGHPRCreate_HeadOverride(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-headoverride")
	client := stubArgus(t, src, wt)
	body := fakeGHCaptureArgv + `
echo "https://github.com/anutron/iris/pull/55"
exit 0
`
	dir := writeFakeGH(t, body)

	result, err := GHPRCreate(context.Background(), client, "task-headoverride", GHPRCreateOptions{Title: "Override test", Head: "feature-x"})
	if err != nil {
		t.Fatalf("gh pr create with head override: %v", err)
	}
	if result.Number != 55 {
		t.Fatalf("PR number: got %d want 55", result.Number)
	}
	argv := readFakeGHArgv(t, dir)
	if !strings.Contains(argv, "feature-x") {
		t.Fatalf("expected head override 'feature-x' in argv:\n%s", argv)
	}
	// The task's resolved branch must NOT appear as the --head value.
	if strings.Contains(argv, "argus/ghpr-headoverride") {
		t.Fatalf("expected task branch NOT to appear in argv when head override given:\n%s", argv)
	}
}

// TestGHPRCreate_HeadOverrideDefaultBranchRefused verifies that the default-branch
// refusal applies to the head override, not just the resolved task branch.
func TestGHPRCreate_HeadOverrideDefaultBranchRefused(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-headoverride-default")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHCaptureArgv+"\nexit 0\n")

	_, err := GHPRCreate(context.Background(), client, "task-headodef", GHPRCreateOptions{Title: "x", Head: "main"})
	if err == nil {
		t.Fatal("expected refusal for default branch via head override, got nil")
	}
	if !strings.Contains(err.Error(), "default branch") {
		t.Fatalf("unexpected error: %v", err)
	}
	if argv := readFakeGHArgv(t, dir); argv != "" {
		t.Fatalf("expected gh NOT to be invoked, but argv was captured:\n%s", argv)
	}
}

// TestGHPRCreate_HeadOverrideRejectsLeadingDash verifies that a caller-supplied
// head override beginning with '-' is rejected before gh runs, so it cannot
// smuggle flags into gh pr create.
func TestGHPRCreate_HeadOverrideRejectsLeadingDash(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-headoverride-dash")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHCaptureArgv+"\nexit 0\n")

	_, err := GHPRCreate(context.Background(), client, "task-headodash", GHPRCreateOptions{Title: "x", Head: "--upload-pack=evil"})
	if err == nil {
		t.Fatal("expected error rejecting leading-dash head override, got nil")
	}
	if !strings.Contains(err.Error(), "must not begin with '-'") {
		t.Fatalf("unexpected error: %v", err)
	}
	if argv := readFakeGHArgv(t, dir); argv != "" {
		t.Fatalf("expected gh NOT to be invoked, but argv was captured:\n%s", argv)
	}
}

// TestGHPRCreate_ForkWithHeadOverride verifies that a fork origin fork-qualifies
// the head override (not the resolved task branch).
func TestGHPRCreate_ForkWithHeadOverride(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-fork-headoverride")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHForkAware(
		`{"nameWithOwner":"anutron/argus","parent":{"name":"argus","owner":{"login":"drn"}}}`,
		"https://github.com/drn/argus/pull/77", 0))

	result, err := GHPRCreate(context.Background(), client, "task-fork-headoverride", GHPRCreateOptions{Title: "Fork override", Head: "feature-x"})
	if err != nil {
		t.Fatalf("cross-fork pr create with head override: %v", err)
	}
	if result.Number != 77 {
		t.Fatalf("PR number: got %d want 77", result.Number)
	}
	argv := readFakeGHArgv(t, dir)
	// The fork-qualified override must appear, not the task branch.
	if !strings.Contains(argv, "anutron:feature-x") {
		t.Fatalf("expected fork-qualified head override 'anutron:feature-x' in argv:\n%s", argv)
	}
	if strings.Contains(argv, "argus/ghpr-fork-headoverride") {
		t.Fatalf("expected task branch NOT to appear in argv:\n%s", argv)
	}
}

// TestGHPRCreate_NoHeadOverridePreservesTaskBranch verifies backward compatibility:
// omitting opts.Head opens the PR for the task's resolved branch as before.
func TestGHPRCreate_NoHeadOverridePreservesTaskBranch(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-noheadoverride")
	client := stubArgus(t, src, wt)
	body := fakeGHCaptureArgv + `
echo "https://github.com/anutron/iris/pull/99"
exit 0
`
	dir := writeFakeGH(t, body)

	result, err := GHPRCreate(context.Background(), client, "task-noheadoverride", GHPRCreateOptions{Title: "No override"})
	if err != nil {
		t.Fatalf("gh pr create: %v", err)
	}
	if result.Number != 99 {
		t.Fatalf("PR number: got %d want 99", result.Number)
	}
	argv := readFakeGHArgv(t, dir)
	if !strings.Contains(argv, "argus/ghpr-noheadoverride") {
		t.Fatalf("expected task branch 'argus/ghpr-noheadoverride' in argv:\n%s", argv)
	}
}

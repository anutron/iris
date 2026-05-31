package verbs

import (
	"context"
	"strings"
	"testing"
)

// fakeGHForkAware builds a fake-gh body that answers `gh repo view --json ...`
// with repoViewJSON and records the `gh pr create` argv (one arg per line) to
// $IRIS_FAKE_GH_DIR/argv before emitting prURL. repoViewExit lets a test make
// the repo-view detection call fail.
func fakeGHForkAware(repoViewJSON, prURL string, repoViewExit int) string {
	rv := "printf '%s' '" + repoViewJSON + "'\n    exit 0"
	if repoViewExit != 0 {
		rv = "echo 'gh: detection failed' >&2\n    exit 1"
	}
	return `
case "$1 $2" in
  "repo view")
    ` + rv + ` ;;
  "pr create")
    { for a in "$@"; do printf '%s\n' "$a"; done; } > "$IRIS_FAKE_GH_DIR/argv"
    printf '%s\n' "` + prURL + `"
    exit 0 ;;
esac
echo "unexpected gh invocation: $*" >&2
exit 1
`
}

func TestGHPRCreate_CrossForkPR(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-fork")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHForkAware(
		`{"nameWithOwner":"anutron/argus","parent":{"name":"argus","owner":{"login":"drn"}}}`,
		"https://github.com/drn/argus/pull/7", 0))

	result, err := GHPRCreate(context.Background(), client, "task-ghpr-fork", GHPRCreateOptions{Title: "Add config"})
	if err != nil {
		t.Fatalf("cross-fork pr create: %v", err)
	}
	if result.Number != 7 {
		t.Fatalf("PR number: got %d want 7", result.Number)
	}
	argv := readFakeGHArgv(t, dir)
	for _, want := range []string{"--repo", "drn/argus", "--head", "anutron:argus/ghpr-fork"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("cross-fork argv missing %q:\n%s", want, argv)
		}
	}
	if strings.Contains(argv, "--base") {
		t.Fatalf("cross-fork PR should omit --base (gh defaults to upstream default):\n%s", argv)
	}
}

func TestGHPRCreate_NonForkSameRepo(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-nonfork")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHForkAware(
		`{"nameWithOwner":"anutron/iris","parent":null}`,
		"https://github.com/anutron/iris/pull/9", 0))

	result, err := GHPRCreate(context.Background(), client, "task-ghpr-nonfork", GHPRCreateOptions{Title: "x"})
	if err != nil {
		t.Fatalf("same-repo pr create: %v", err)
	}
	if result.Number != 9 {
		t.Fatalf("PR number: got %d want 9", result.Number)
	}
	argv := readFakeGHArgv(t, dir)
	if !strings.Contains(argv, "--base") || !strings.Contains(argv, "--head") {
		t.Fatalf("same-repo argv should keep --base/--head:\n%s", argv)
	}
	if strings.Contains(argv, "--repo") || strings.Contains(argv, "anutron:") {
		t.Fatalf("non-fork PR must not add --repo or a fork-qualified head:\n%s", argv)
	}
}

func TestGHPRCreate_ForkDetectionFailureFallsBack(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-detectfail")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHForkAware(
		"", "https://github.com/anutron/iris/pull/11", 1)) // repo view exits non-zero

	result, err := GHPRCreate(context.Background(), client, "task-ghpr-detectfail", GHPRCreateOptions{Title: "x"})
	if err != nil {
		t.Fatalf("detection failure should fall back to same-repo, not error: %v", err)
	}
	if result.Number != 11 {
		t.Fatalf("PR number: got %d want 11", result.Number)
	}
	argv := readFakeGHArgv(t, dir)
	if !strings.Contains(argv, "--base") {
		t.Fatalf("fallback should use same-repo --base form:\n%s", argv)
	}
	if strings.Contains(argv, "--repo") {
		t.Fatalf("fallback must not add --repo:\n%s", argv)
	}
}

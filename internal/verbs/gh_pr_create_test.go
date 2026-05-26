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
	client := stubArgusTaskNotFound(t) // never reached
	_, err := GHPRCreate(context.Background(), client, "task-x", GHPRCreateOptions{})
	if err == nil {
		t.Fatal("expected error for missing title, got nil")
	}
	if !strings.Contains(err.Error(), "title") {
		t.Fatalf("expected title-required error, got: %v", err)
	}
}

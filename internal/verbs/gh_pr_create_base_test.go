package verbs

import (
	"context"
	"strings"
	"testing"
)

// Delta scenario: "Explicit base overrides the target branch in same-repo mode."
func TestGHPRCreate_BaseOverridesSameRepoTarget(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-base-samerepo")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHForkAware(
		`{"nameWithOwner":"anutron/iris","parent":null}`,
		"https://github.com/anutron/iris/pull/21", 0))

	result, err := GHPRCreate(context.Background(), client, "task-ghpr-base-samerepo", GHPRCreateOptions{
		Title: "x",
		Base:  "integration/big-feature",
	})
	if err != nil {
		t.Fatalf("gh pr create with base: %v", err)
	}
	if result.Number != 21 {
		t.Fatalf("PR number: got %d want 21", result.Number)
	}
	argv := readFakeGHArgv(t, dir)
	if !strings.Contains(argv, "--base") || !strings.Contains(argv, "integration/big-feature") {
		t.Fatalf("expected --base integration/big-feature in argv:\n%s", argv)
	}
	if strings.Contains(argv, "--repo") {
		t.Fatalf("same-repo mode must not add --repo:\n%s", argv)
	}
}

// Delta scenario: "Explicit base composes with base_repo."
func TestGHPRCreate_BaseComposesWithBaseRepo(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-base-baserepo")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHBaseRepoOnly("https://github.com/drn/argus/pull/22"))

	result, err := GHPRCreate(context.Background(), client, "task-ghpr-base-baserepo", GHPRCreateOptions{
		Title:    "x",
		BaseRepo: "drn/argus",
		Base:     "release/1.2",
	})
	if err != nil {
		t.Fatalf("gh pr create with base_repo+base: %v", err)
	}
	if result.Number != 22 {
		t.Fatalf("PR number: got %d want 22", result.Number)
	}
	argv := readFakeGHArgv(t, dir)
	for _, want := range []string{"--repo", "drn/argus", "--base", "release/1.2", "--head", "argus/ghpr-base-baserepo"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("argv missing %q:\n%s", want, argv)
		}
	}
	if strings.Contains(argv, "drn:") || strings.Contains(argv, "anutron:") {
		t.Fatalf("base_repo PR must NOT fork-qualify the head:\n%s", argv)
	}
}

// Delta scenario: "Explicit base composes with cross-fork auto-detection."
func TestGHPRCreate_BaseComposesWithCrossFork(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-base-fork")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHForkAware(
		`{"nameWithOwner":"anutron/argus","parent":{"name":"argus","owner":{"login":"drn"}}}`,
		"https://github.com/drn/argus/pull/23", 0))

	result, err := GHPRCreate(context.Background(), client, "task-ghpr-base-fork", GHPRCreateOptions{
		Title: "x",
		Base:  "release/1.2",
	})
	if err != nil {
		t.Fatalf("cross-fork pr create with base: %v", err)
	}
	if result.Number != 23 {
		t.Fatalf("PR number: got %d want 23", result.Number)
	}
	argv := readFakeGHArgv(t, dir)
	for _, want := range []string{"--repo", "drn/argus", "--base", "release/1.2", "--head", "anutron:argus/ghpr-base-fork"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("cross-fork argv missing %q:\n%s", want, argv)
		}
	}
}

// Delta scenario: "Omitted base preserves existing per-mode default-branch behavior."
func TestGHPRCreate_OmittedBasePreservesExistingBehavior(t *testing.T) {
	t.Run("same-repo", func(t *testing.T) {
		src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-nobase-samerepo")
		client := stubArgus(t, src, wt)
		dir := writeFakeGH(t, fakeGHForkAware(
			`{"nameWithOwner":"anutron/iris","parent":null}`,
			"https://github.com/anutron/iris/pull/24", 0))

		if _, err := GHPRCreate(context.Background(), client, "task-ghpr-nobase-samerepo", GHPRCreateOptions{Title: "x"}); err != nil {
			t.Fatalf("gh pr create: %v", err)
		}
		argv := readFakeGHArgv(t, dir)
		if !strings.Contains(argv, "--base") || !strings.Contains(argv, "main") {
			t.Fatalf("expected --base main (default branch) preserved:\n%s", argv)
		}
	})

	t.Run("base_repo", func(t *testing.T) {
		src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-nobase-baserepo")
		client := stubArgus(t, src, wt)
		dir := writeFakeGH(t, fakeGHBaseRepoOnly("https://github.com/drn/argus/pull/25"))

		if _, err := GHPRCreate(context.Background(), client, "task-ghpr-nobase-baserepo", GHPRCreateOptions{
			Title:    "x",
			BaseRepo: "drn/argus",
		}); err != nil {
			t.Fatalf("gh pr create: %v", err)
		}
		argv := readFakeGHArgv(t, dir)
		if strings.Contains(argv, "--base") {
			t.Fatalf("expected --base omitted when base_repo set with no base override:\n%s", argv)
		}
	})

	t.Run("cross-fork", func(t *testing.T) {
		src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-nobase-fork")
		client := stubArgus(t, src, wt)
		dir := writeFakeGH(t, fakeGHForkAware(
			`{"nameWithOwner":"anutron/argus","parent":{"name":"argus","owner":{"login":"drn"}}}`,
			"https://github.com/drn/argus/pull/26", 0))

		if _, err := GHPRCreate(context.Background(), client, "task-ghpr-nobase-fork", GHPRCreateOptions{Title: "x"}); err != nil {
			t.Fatalf("gh pr create: %v", err)
		}
		argv := readFakeGHArgv(t, dir)
		if strings.Contains(argv, "--base") {
			t.Fatalf("expected --base omitted in cross-fork mode with no base override:\n%s", argv)
		}
	})
}

// Delta scenario: "Rejects a base beginning with a dash."
func TestGHPRCreate_BaseRejectsLeadingDash(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghpr-base-dash")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHCaptureArgv+"\nexit 0\n")

	_, err := GHPRCreate(context.Background(), client, "task-ghpr-base-dash", GHPRCreateOptions{
		Title: "x",
		Base:  "--upload-pack=evil",
	})
	if err == nil {
		t.Fatal("expected error rejecting leading-dash base, got nil")
	}
	if !strings.Contains(err.Error(), "must not begin with '-'") {
		t.Fatalf("unexpected error: %v", err)
	}
	if argv := readFakeGHArgv(t, dir); argv != "" {
		t.Fatalf("expected gh NOT to be invoked, but argv was captured:\n%s", argv)
	}
}

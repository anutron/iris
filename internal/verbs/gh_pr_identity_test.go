package verbs

import (
	"context"
	"strings"
	"testing"
)

// fakeGHPRIdentity records every gh call and answers `gh pr view` with a PR
// under acme/video-gen titled "Add thumbnails"; merge and close succeed.
const fakeGHPRIdentity = fakeGHRecordCalls + `
echo "A new release of gh is available: 2.80.0" >&2
case "$2" in
  view)  printf '%s' '{"url":"https://github.com/acme/video-gen/pull/11","title":"Add thumbnails"}'; exit 0 ;;
  merge) exit 0 ;;
  close) exit 0 ;;
esac
echo "unexpected gh invocation: $*" >&2
exit 1
`

// fakeGHViewFails fails `gh pr view` but lets merge and close succeed.
const fakeGHViewFails = fakeGHRecordCalls + `
case "$2" in
  view)  echo "GraphQL: Could not resolve to a PullRequest" >&2; exit 1 ;;
  merge) exit 0 ;;
  close) exit 0 ;;
esac
exit 1
`

func TestGHPRMerge_ReportsPRIdentity(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghmerge-identity")
	client := stubArgus(t, src, wt)
	writeFakeGH(t, fakeGHPRIdentity)

	result, err := GHPRMerge(context.Background(), client, "task", GHPRMergeOptions{PRNumber: 11, Strategy: "squash"})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if result.PRURL != "https://github.com/acme/video-gen/pull/11" || result.PRTitle != "Add thumbnails" {
		t.Fatalf("identity missing: %+v", result)
	}
}

func TestGHPRMerge_MatchingExpectedRepoMerges(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghmerge-expect-ok")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHPRIdentity)

	result, err := GHPRMerge(context.Background(), client, "task", GHPRMergeOptions{
		PRNumber: 11, Strategy: "squash", ExpectedRepo: "ACME/video-gen",
	})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !result.Merged {
		t.Fatal("expected merged=true")
	}
	if !strings.Contains(readFakeGHCalls(t, dir), "CALL pr merge 11 --squash --repo acme/video-gen") {
		t.Fatalf("merge not invoked:\n%s", readFakeGHCalls(t, dir))
	}
}

func TestGHPRMerge_MismatchedExpectedRepoRefuses(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghmerge-expect-bad")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHPRIdentity)

	_, err := GHPRMerge(context.Background(), client, "task", GHPRMergeOptions{
		PRNumber: 11, Strategy: "squash", ExpectedRepo: "acme/sketch",
	})
	if err == nil {
		t.Fatal("expected refusal on repo mismatch")
	}
	for _, want := range []string{"acme/sketch", "acme/video-gen", "Add thumbnails"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error should name %q: %v", want, err)
		}
	}
	if strings.Contains(readFakeGHCalls(t, dir), "pr merge") {
		t.Fatalf("gh pr merge must not run on mismatch:\n%s", readFakeGHCalls(t, dir))
	}
}

func TestGHPRMerge_UnidentifiablePRWithExpectedRepoRefuses(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghmerge-expect-noview")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHViewFails)

	_, err := GHPRMerge(context.Background(), client, "task", GHPRMergeOptions{
		PRNumber: 11, Strategy: "squash", ExpectedRepo: "acme/video-gen",
	})
	if err == nil {
		t.Fatal("expected refusal when the PR can't be identified")
	}
	if strings.Contains(readFakeGHCalls(t, dir), "pr merge") {
		t.Fatalf("gh pr merge must not run:\n%s", readFakeGHCalls(t, dir))
	}
}

func TestGHPRMerge_LookupFailureWithoutGuardStillMerges(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghmerge-noview")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHViewFails)

	result, err := GHPRMerge(context.Background(), client, "task", GHPRMergeOptions{PRNumber: 11, Strategy: "squash"})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !result.Merged || !strings.Contains(readFakeGHCalls(t, dir), "pr merge") {
		t.Fatal("merge should proceed without a guard")
	}
	if !strings.Contains(strings.Join(result.Warnings, "\n"), "could not identify PR #11") {
		t.Fatalf("expected identification warning, got %v", result.Warnings)
	}
}

func TestGHPRMerge_MalformedExpectedRepoRejectedBeforeGH(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t) // never reached
	_, err := GHPRMerge(context.Background(), client, "task", GHPRMergeOptions{
		PRNumber: 11, Strategy: "squash", ExpectedRepo: "video-gen",
	})
	if err == nil || !strings.Contains(err.Error(), "expected_repo") {
		t.Fatalf("expected expected_repo validation error, got %v", err)
	}
}

func TestGHPRClose_ReportsPRIdentity(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghclose-identity")
	client := stubArgus(t, src, wt)
	writeFakeGH(t, fakeGHPRIdentity)

	result, err := GHPRClose(context.Background(), client, "task", GHPRCloseOptions{PRNumber: 11})
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if result.PRURL != "https://github.com/acme/video-gen/pull/11" || result.PRTitle != "Add thumbnails" {
		t.Fatalf("identity missing: %+v", result)
	}
}

func TestGHPRClose_MismatchedExpectedRepoRefuses(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghclose-expect-bad")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHPRIdentity)

	_, err := GHPRClose(context.Background(), client, "task", GHPRCloseOptions{PRNumber: 11, ExpectedRepo: "acme/sketch"})
	if err == nil || !strings.Contains(err.Error(), "acme/video-gen") {
		t.Fatalf("expected mismatch refusal naming the PR's repo, got %v", err)
	}
	if strings.Contains(readFakeGHCalls(t, dir), "pr close") {
		t.Fatalf("gh pr close must not run on mismatch:\n%s", readFakeGHCalls(t, dir))
	}
}

func TestGHPRClose_LookupFailureWithoutGuardStillCloses(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghclose-noview")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHViewFails)

	result, err := GHPRClose(context.Background(), client, "task", GHPRCloseOptions{PRNumber: 11})
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if !result.Closed || result.PRURL != "" || !strings.Contains(readFakeGHCalls(t, dir), "pr close") {
		t.Fatalf("close should proceed with empty identity: %+v", result)
	}
}

func TestGHPRClose_BindsCloseToVerifiedRepo(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghclose-bind")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHPRIdentity)

	if _, err := GHPRClose(context.Background(), client, "task", GHPRCloseOptions{PRNumber: 11, ExpectedRepo: "acme/video-gen"}); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !strings.Contains(readFakeGHCalls(t, dir), "CALL pr close 11 --repo acme/video-gen") {
		t.Fatalf("close not bound to verified repo:\n%s", readFakeGHCalls(t, dir))
	}
}

func TestGHPRMerge_LookupFailureLeavesRepoUnbound(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "ghmerge-unbound")
	client := stubArgus(t, src, wt)
	dir := writeFakeGH(t, fakeGHViewFails)

	if _, err := GHPRMerge(context.Background(), client, "task", GHPRMergeOptions{PRNumber: 11, Strategy: "squash"}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if strings.Contains(readFakeGHCalls(t, dir), "--repo") {
		t.Fatalf("no verified repo, so gh must resolve as before:\n%s", readFakeGHCalls(t, dir))
	}
}

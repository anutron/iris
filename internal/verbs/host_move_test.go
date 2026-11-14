package verbs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anutron/iris/internal/argus"
)

// advanceOriginMain simulates a remote PR merge by committing to origin/main
// from a separate clone.
func advanceOriginMain(t *testing.T, bare string) string {
	t.Helper()
	g := gitRunner(t)
	other := filepath.Join(t.TempDir(), "other")
	g("", "clone", bare, other)
	g(other, "config", "user.email", "o@example.com")
	g(other, "config", "user.name", "o")
	g(other, "commit", "--allow-empty", "-m", "remote merge")
	g(other, "push", "origin", "main")
	return strings.TrimSpace(g(other, "rev-parse", "HEAD"))
}

func fakeGHMergeOK(t *testing.T) {
	t.Helper()
	writeFakeGH(t, fakeGHCaptureArgv+"\necho merged\nexit 0\n")
}

func warningsContain(ws []string, sub string) bool {
	for _, w := range ws {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

func hostBranch(t *testing.T, src string) string {
	t.Helper()
	return strings.TrimSpace(gitRunner(t)(src, "rev-parse", "--abbrev-ref", "HEAD"))
}

// leaseFixtureOn is leaseFixture with the host checkout on the dogfood branch
// ("dev"), as on a live dogfood host.
func leaseFixtureOn(t *testing.T, slug string) (src, wt, sha string, f *fakeLeaseArgus, c *argus.Client) {
	t.Helper()
	src, wt, sha, f, c = leaseFixture(t, slug)
	gitRunner(t)(src, "checkout", "-b", "dev")
	return
}

func TestGHPRMerge_HostOnDogfoodBranchStaysPut(t *testing.T) {
	src, wt, bare := setupRepoWithBareAndWorktree(t, "hm-dev")
	client := stubArgus(t, src, wt)
	g := gitRunner(t)
	g(src, "checkout", "-b", "dev")
	devHead := revParse(t, src, "HEAD")
	fakeGHMergeOK(t)
	newMain := advanceOriginMain(t, bare)

	res, err := GHPRMerge(context.Background(), client, "task-x", GHPRMergeOptions{PRNumber: 1, Strategy: "squash"})
	if err != nil {
		t.Fatal(err)
	}
	if b := hostBranch(t, src); b != "dev" {
		t.Fatalf("host moved to %q, want dev", b)
	}
	if got := revParse(t, src, "HEAD"); got != devHead {
		t.Fatalf("dev HEAD changed: %s -> %s", devHead, got)
	}
	if got := revParse(t, src, "refs/heads/main"); got != newMain {
		t.Fatalf("local main = %s, want fast-forward to %s (warnings: %v)", got, newMain, res.Warnings)
	}
}

func TestGHPRMerge_HostOnDefaultFastForwards(t *testing.T) {
	src, wt, bare := setupRepoWithBareAndWorktree(t, "hm-main")
	client := stubArgus(t, src, wt)
	fakeGHMergeOK(t)
	newMain := advanceOriginMain(t, bare)

	if _, err := GHPRMerge(context.Background(), client, "task-x", GHPRMergeOptions{PRNumber: 1, Strategy: "squash"}); err != nil {
		t.Fatal(err)
	}
	if got := revParse(t, src, "HEAD"); got != newMain {
		t.Fatalf("HEAD = %s, want %s", got, newMain)
	}
}

func TestGHPRMerge_DivergedLocalMainWarnsAndStays(t *testing.T) {
	src, wt, bare := setupRepoWithBareAndWorktree(t, "hm-div")
	client := stubArgus(t, src, wt)
	g := gitRunner(t)
	g(src, "commit", "--allow-empty", "-m", "local only")
	localMain := revParse(t, src, "refs/heads/main")
	g(src, "checkout", "-b", "dev")
	fakeGHMergeOK(t)
	advanceOriginMain(t, bare)

	res, err := GHPRMerge(context.Background(), client, "task-x", GHPRMergeOptions{PRNumber: 1, Strategy: "squash"})
	if err != nil {
		t.Fatal(err)
	}
	if got := revParse(t, src, "refs/heads/main"); got != localMain {
		t.Fatalf("diverged main moved: %s -> %s", localMain, got)
	}
	if b := hostBranch(t, src); b != "dev" {
		t.Fatalf("host moved to %q", b)
	}
	if !warningsContain(res.Warnings, "diverged") {
		t.Fatalf("want a diverged warning, got %v", res.Warnings)
	}
}

func TestGHPRMerge_DefaultHeldByWorktreeIsSkipped(t *testing.T) {
	src, wt, bare := setupRepoWithBareAndWorktree(t, "hm-wt")
	client := stubArgus(t, src, wt)
	g := gitRunner(t)
	g(src, "checkout", "-b", "dev")
	g(src, "worktree", "add", filepath.Join(t.TempDir(), "mainwt"), "main")
	before := revParse(t, src, "refs/heads/main")
	fakeGHMergeOK(t)
	advanceOriginMain(t, bare)

	res, err := GHPRMerge(context.Background(), client, "task-x", GHPRMergeOptions{PRNumber: 1, Strategy: "squash"})
	if err != nil {
		t.Fatal(err)
	}
	if got := revParse(t, src, "refs/heads/main"); got != before {
		t.Fatal("worktree-held main moved")
	}
	if !warningsContain(res.Warnings, "checked out") {
		t.Fatalf("want a worktree warning, got %v", res.Warnings)
	}
}

func TestGHPRMerge_PostMergeSyncHoldsSourceRepoLock(t *testing.T) {
	src, wt, bare := setupRepoWithBareAndWorktree(t, "hm-lock")
	client := stubArgus(t, src, wt)
	fakeGHMergeOK(t)
	advanceOriginMain(t, bare)
	canon, _ := filepath.EvalSymlinks(src)

	mu := lockSourceRepo(canon)
	done := make(chan struct{})
	go func() {
		_, _ = GHPRMerge(context.Background(), client, "task-x", GHPRMergeOptions{PRNumber: 1, Strategy: "squash"})
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("GHPRMerge finished while the source-repo lock was held")
	case <-time.After(300 * time.Millisecond):
	}
	mu.Unlock()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("GHPRMerge did not finish after the lock was released")
	}
}

// ---- lease gate for deliberate host movers -----------------------------------

func TestCheckout_LeaseHeldByOtherRefusedWhenLeavingDogfood(t *testing.T) {
	src, _, _, _, c := leaseFixtureOn(t, "hm-co-refuse")
	claim(t, c, "A", 0)

	_, err := Checkout(context.Background(), CheckoutInput{Client: c, TaskID: "B", Branch: "main"})
	var ge *LeaseGateError
	if !errors.As(err, &ge) || ge.Holder == nil || ge.Holder.TaskID != "A" {
		t.Fatalf("err = %v, want LeaseGateError naming A", err)
	}
	if b := hostBranch(t, src); b != "dev" {
		t.Fatalf("host moved to %q", b)
	}
}

func TestCheckout_OverrideProceedsWithWarning(t *testing.T) {
	src, _, _, _, c := leaseFixtureOn(t, "hm-co-override")
	claim(t, c, "A", 0)

	res, err := Checkout(context.Background(), CheckoutInput{Client: c, TaskID: "B", Branch: "main", LeaseOverride: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) == 0 || !strings.HasPrefix(res.Warnings[0], "LEASE OVERRIDE:") {
		t.Fatalf("warnings = %v", res.Warnings)
	}
	if b := hostBranch(t, src); b != "main" {
		t.Fatalf("host on %q", b)
	}
}

func TestCheckout_HolderAndFreeLeasePass(t *testing.T) {
	src, _, _, _, c := leaseFixtureOn(t, "hm-co-pass")
	claim(t, c, "A", 0)
	if _, err := Checkout(context.Background(), CheckoutInput{Client: c, TaskID: "A", Branch: "main"}); err != nil {
		t.Fatalf("holder: %v", err)
	}
	gitRunner(t)(src, "checkout", "dev")
	if _, err := DogfoodRelease(context.Background(), c, "A"); err != nil {
		t.Fatal(err)
	}
	if _, err := Checkout(context.Background(), CheckoutInput{Client: c, TaskID: "B", Branch: "main"}); err != nil {
		t.Fatalf("free lease: %v", err)
	}
}

func TestCheckout_ReturningToDogfoodBranchIsNotGated(t *testing.T) {
	src, _, _, _, c := leaseFixtureOn(t, "hm-co-todev")
	claim(t, c, "A", 0)
	gitRunner(t)(src, "checkout", "main")
	if _, err := Checkout(context.Background(), CheckoutInput{Client: c, TaskID: "B", Branch: "dev"}); err != nil {
		t.Fatalf("returning to dogfood branch must pass: %v", err)
	}
}

func TestCherryPick_OntoDogfoodBranchByNonHolderRefused(t *testing.T) {
	src, _, _, _, c := leaseFixtureOn(t, "hm-cp-refuse")
	g := gitRunner(t)
	g(src, "checkout", "main")
	g(src, "checkout", "-b", "feat")
	if err := os.WriteFile(filepath.Join(src, "picked.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	g(src, "add", "picked.txt")
	g(src, "commit", "-m", "pick me")
	pick := revParse(t, src, "HEAD")
	g(src, "checkout", "main")
	devBefore := revParse(t, src, "refs/heads/dev")
	claim(t, c, "A", 0)

	_, err := CherryPick(context.Background(), CherryPickInput{Client: c, TaskID: "B", Commit: pick, TargetBranch: "dev"})
	var ge *LeaseGateError
	if !errors.As(err, &ge) {
		t.Fatalf("err = %v, want LeaseGateError", err)
	}
	if revParse(t, src, "refs/heads/dev") != devBefore || hostBranch(t, src) != "main" {
		t.Fatal("refusal had side effects")
	}

	res, err := CherryPick(context.Background(), CherryPickInput{Client: c, TaskID: "B", Commit: pick, TargetBranch: "dev", LeaseOverride: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) == 0 || !strings.HasPrefix(res.Warnings[0], "LEASE OVERRIDE:") {
		t.Fatalf("warnings = %v", res.Warnings)
	}
}

func TestCherryPick_UnrelatedBranchesNotGated(t *testing.T) {
	src, _, _, _, c := leaseFixtureOn(t, "hm-cp-free")
	g := gitRunner(t)
	g(src, "checkout", "main")
	g(src, "branch", "other")
	g(src, "checkout", "-b", "feat")
	if err := os.WriteFile(filepath.Join(src, "picked.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	g(src, "add", "picked.txt")
	g(src, "commit", "-m", "pick me")
	pick := revParse(t, src, "HEAD")
	g(src, "checkout", "main")
	claim(t, c, "A", 0)
	if _, err := CherryPick(context.Background(), CherryPickInput{Client: c, TaskID: "B", Commit: pick, TargetBranch: "other"}); err != nil {
		t.Fatalf("unrelated pick gated: %v", err)
	}
}

func TestMergeToMaster_NonHolderRefusedWhileHostServesDogfood(t *testing.T) {
	src, _, _, _, c := leaseFixtureOn(t, "hm-mm-refuse")
	claim(t, c, "A", 0)
	var ge *LeaseGateError
	if _, err := MergeToMaster(context.Background(), c, "B", MergeOptions{NoFF: true}); !errors.As(err, &ge) {
		t.Fatalf("err = %v, want LeaseGateError", err)
	}
	if _, err := MergeToMaster(context.Background(), c, "B", MergeOptions{NoFF: true, DryRun: true}); !errors.As(err, &ge) {
		t.Fatalf("dry-run err = %v, want LeaseGateError", err)
	}
	if hostBranch(t, src) != "dev" {
		t.Fatal("host moved")
	}
}

func TestCompleteTask_NonHolderRefusedWhileHostServesDogfood(t *testing.T) {
	src, _, _, _, c := leaseFixtureOn(t, "hm-ct-refuse")
	claim(t, c, "A", 0)
	res, err := CompleteTask(context.Background(), c, "B", CompleteTaskOptions{})
	var ge *LeaseGateError
	if !errors.As(err, &ge) {
		t.Fatalf("err = %v, want LeaseGateError", err)
	}
	if res != nil && len(res.Checkpoints) != 0 {
		t.Fatalf("checkpoints reached: %v", res.Checkpoints)
	}
	if hostBranch(t, src) != "dev" {
		t.Fatal("host moved")
	}
}

func TestStatus_HostOffDogfoodFlag(t *testing.T) {
	src, _, _, _, c := leaseFixtureOn(t, "hm-status")
	res, err := Status(context.Background(), c, StatusInput{TaskID: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if res.DogfoodBranch != "dev" || res.HostOffDogfood {
		t.Fatalf("on dev: branch=%q off=%v", res.DogfoodBranch, res.HostOffDogfood)
	}
	gitRunner(t)(src, "checkout", "main")
	res, err = Status(context.Background(), c, StatusInput{TaskID: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.HostOffDogfood || !warningsContain(res.Warnings, "main") || !warningsContain(res.Warnings, "dev") {
		t.Fatalf("off=%v warnings=%v", res.HostOffDogfood, res.Warnings)
	}
}

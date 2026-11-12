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

// ---- set_dogfood lease gate --------------------------------------------------

func TestSetDogfoodGate_NonHolderRefusedWithoutSideEffects(t *testing.T) {
	src, _, sha, _, c := leaseFixture(t, "gate-refuse")
	claim(t, c, "A", 0)
	claim(t, c, "B", 0)

	_, err := SetDogfood(context.Background(), c, "B", SetDogfoodOpts{Sha: sha, Manifest: sampleManifest()})
	var ge *LeaseGateError
	if !errors.As(err, &ge) {
		t.Fatalf("err = %v, want *LeaseGateError", err)
	}
	if ge.Holder == nil || ge.Holder.TaskID != "A" || ge.Holder.TaskName != "task-a" || ge.Position != 1 || len(ge.Ahead) != 0 {
		t.Fatalf("structured fields = %+v", ge)
	}
	for _, want := range []string{"task-a", "iris_dogfood_claim", "lease_override", "position 1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
	// No manifest, no ref, no checkout change.
	stateDir, _ := SourceRepoStateDir(canonicalize(src))
	if m, _ := ReadManifest(stateDir); m != nil {
		t.Fatal("manifest written on refusal")
	}
	if _, err := runGit(context.Background(), src, "rev-parse", "--verify", "--quiet", "refs/heads/dev"); err == nil {
		t.Fatal("dev branch created on refusal")
	}
	if b := currentBranchOrFail(t, src); b != "main" {
		t.Fatalf("source repo moved to %q", b)
	}
}

func TestSetDogfoodGate_UnqueuedCallerSeesWholeQueue(t *testing.T) {
	_, _, sha, _, c := leaseFixture(t, "gate-ahead")
	claim(t, c, "A", 0)
	claim(t, c, "B", 0)
	claim(t, c, "C", 0)
	_, err := SetDogfood(context.Background(), c, "D", SetDogfoodOpts{Sha: sha, Manifest: sampleManifest()})
	var ge *LeaseGateError
	if !errors.As(err, &ge) || len(ge.Ahead) != 2 || ge.Ahead[0].TaskID != "B" || ge.Ahead[1].TaskID != "C" || ge.Position != 0 {
		t.Fatalf("err = %v / %+v", err, ge)
	}
	if !strings.Contains(err.Error(), "task-b") || !strings.Contains(err.Error(), "task-c") || !strings.Contains(err.Error(), "task-a") {
		t.Fatalf("error should name holder and everyone ahead: %v", err)
	}
}

func TestSetDogfoodGate_FreeLeaseStillRequiresClaim(t *testing.T) {
	_, _, sha, _, c := leaseFixture(t, "gate-free")
	_, err := SetDogfood(context.Background(), c, "A", SetDogfoodOpts{Sha: sha, Manifest: sampleManifest()})
	var ge *LeaseGateError
	if !errors.As(err, &ge) || ge.Holder != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "no one holds") || !strings.Contains(err.Error(), "iris_dogfood_claim") {
		t.Fatalf("err = %v", err)
	}
}

func TestSetDogfoodGate_HolderDeploysAndCountsAsContact(t *testing.T) {
	src, _, sha, _, c := leaseFixture(t, "gate-holder")
	canon := canonicalize(src)
	g := gitRunner(t)
	g(src, "branch", "dev", sha+"^")
	claim(t, c, "A", 0)
	ageLease(t, canon, 8*time.Minute)
	before, _ := loadLease(canon)

	res, err := SetDogfood(context.Background(), c, "A", SetDogfoodOpts{Sha: sha, Manifest: sampleManifest()})
	if err != nil {
		t.Fatalf("holder deploy: %v", err)
	}
	for _, w := range res.Warnings {
		if strings.Contains(w, "LEASE") {
			t.Fatalf("holder must see no lease warning: %v", res.Warnings)
		}
	}
	after, _ := loadLease(canon)
	if !after.Holder.LastContactAt.After(before.Holder.LastContactAt) || after.Holder.InFlight != 0 {
		t.Fatalf("contact/in-flight: before=%+v after=%+v", before.Holder, after.Holder)
	}
	if after.Holder.TaskID != "A" {
		t.Fatal("holder keeps the lease")
	}
}

func TestSetDogfoodGate_FailedDeployStillEndsInFlightAndCountsContact(t *testing.T) {
	src, _, sha, _, c := leaseFixture(t, "gate-fail")
	canon := canonicalize(src)
	g := gitRunner(t)
	// dev is ahead of sha → deploying sha drops a commit → refused (no force).
	x := strings.TrimSpace(g(src, "commit-tree", sha+"^{tree}", "-p", sha, "-m", "x"))
	g(src, "branch", "dev", x)
	claim(t, c, "A", 0)
	ageLease(t, canon, 8*time.Minute)
	before, _ := loadLease(canon)
	if _, err := SetDogfood(context.Background(), c, "A", SetDogfoodOpts{Sha: sha, Manifest: sampleManifest()}); err == nil {
		t.Fatal("expected the ancestry refusal")
	}
	after, _ := loadLease(canon)
	if after.Holder.InFlight != 0 || !after.Holder.LastContactAt.After(before.Holder.LastContactAt) {
		t.Fatalf("a failed call must still end in-flight and count as contact: %+v", after.Holder)
	}
}

func TestSetDogfoodGate_LeaseOverrideDeploysWarnsAndTellsHolder(t *testing.T) {
	src, _, sha, f, c := leaseFixture(t, "gate-override")
	canon := canonicalize(src)
	g := gitRunner(t)
	g(src, "branch", "dev", sha+"^")
	claim(t, c, "A", 0)
	claim(t, c, "C", 0)

	res, err := SetDogfood(context.Background(), c, "B", SetDogfoodOpts{Sha: sha, Manifest: sampleManifest(), LeaseOverride: true})
	if err != nil {
		t.Fatalf("override deploy: %v", err)
	}
	if len(res.Warnings) == 0 || !strings.HasPrefix(res.Warnings[0], "LEASE OVERRIDE:") || !strings.Contains(res.Warnings[0], "task-a") {
		t.Fatalf("warnings = %v", res.Warnings)
	}
	ns := f.notifiesTo("A")
	last := ns[len(ns)-1]
	if !strings.Contains(last.Text, "task-b") || !strings.Contains(last.Text, sha) || !strings.Contains(last.Text, "lease_override") {
		t.Fatalf("holder notice = %q", last.Text)
	}
	st, _ := loadLease(canon)
	if st.Holder.TaskID != "A" || len(st.Queue) != 1 || st.Queue[0].TaskID != "C" {
		t.Fatalf("lease must be unchanged: %+v", st)
	}
}

func TestSetDogfoodGate_OverrideWithNoHolderSaysSo(t *testing.T) {
	src, _, sha, _, c := leaseFixture(t, "gate-override-free")
	gitRunner(t)(src, "branch", "dev", sha+"^")
	res, err := SetDogfood(context.Background(), c, "B", SetDogfoodOpts{Sha: sha, Manifest: sampleManifest(), LeaseOverride: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Warnings[0], "LEASE OVERRIDE:") || !strings.Contains(res.Warnings[0], "no holder") {
		t.Fatalf("warnings = %v", res.Warnings)
	}
}

func TestSetDogfoodGate_OverrideAndForceAreIndependent(t *testing.T) {
	src, _, sha, _, c := leaseFixture(t, "gate-indep")
	g := gitRunner(t)
	x := strings.TrimSpace(g(src, "commit-tree", sha+"^{tree}", "-p", sha, "-m", "x"))
	g(src, "branch", "dev", x) // deploying sha would drop x
	claim(t, c, "A", 0)

	// lease_override does not relax the ancestry refusal.
	_, err := SetDogfood(context.Background(), c, "B", SetDogfoodOpts{Sha: sha, Manifest: sampleManifest(), LeaseOverride: true})
	if err == nil || !strings.Contains(err.Error(), "would drop") {
		t.Fatalf("lease_override alone must still hit the commit-dropping refusal: %v", err)
	}
	// force does not relax the lease gate.
	_, err = SetDogfood(context.Background(), c, "B", SetDogfoodOpts{Sha: sha, Manifest: sampleManifest(), Force: true})
	var ge *LeaseGateError
	if !errors.As(err, &ge) {
		t.Fatalf("force alone must still hit the lease gate: %v", err)
	}
	// both together deploy.
	res, err := SetDogfood(context.Background(), c, "B", SetDogfoodOpts{Sha: sha, Manifest: sampleManifest(), Force: true, LeaseOverride: true})
	if err != nil {
		t.Fatalf("both flags: %v", err)
	}
	if !strings.HasPrefix(res.Warnings[0], "LEASE OVERRIDE:") || !strings.HasPrefix(res.Warnings[1], "FORCED") {
		t.Fatalf("warnings order = %v", res.Warnings)
	}
}

func TestSetDogfoodGate_NoTaskIDNeedsOverride(t *testing.T) {
	src, _, sha, _, c := leaseFixture(t, "gate-cli")
	gitRunner(t)(src, "branch", "dev", sha+"^")
	bin := filepath.Join(src, "bin", "iris")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	_ = os.WriteFile(bin, []byte("x"), 0o755)
	// Keep the fake binary out of `git status` so the reload pre-flight passes.
	_ = os.WriteFile(filepath.Join(src, ".git", "info", "exclude"), []byte("bin/\n"), 0o644)
	old := executable
	executable = func() (string, error) { return bin, nil }
	t.Cleanup(func() { executable = old })
	// A real holder exists; a task-less caller can never be it.
	claim(t, c, "A", 0)

	_, err := SetDogfood(context.Background(), c, "", SetDogfoodOpts{Sha: sha, Manifest: sampleManifest()})
	var ge *LeaseGateError
	if !errors.As(err, &ge) {
		t.Fatalf("err = %v, want the lease refusal", err)
	}
	// With the override the call passes the gate. A self-target deploy then
	// needs an exit_code restart (which a test must not trigger), so it stops at
	// the reload step; what matters is that it is no longer the lease refusal.
	_, err = SetDogfood(context.Background(), c, "", SetDogfoodOpts{Sha: sha, Manifest: sampleManifest(), LeaseOverride: true})
	if errors.As(err, &ge) {
		t.Fatalf("lease_override must pass the lease gate: %v", err)
	}
	stateDir, _ := SourceRepoStateDir(canonicalize(src))
	if m, _ := ReadManifest(stateDir); m == nil {
		t.Fatal("the override call should have proceeded past the gate to the manifest write")
	}
}

func TestSetDogfoodGate_HolderNotReleasedMidDeploy(t *testing.T) {
	// The holder's in-flight call defers every timer release; verified through
	// the persisted state while a deploy is running (the build blocks on a file).
	src, _, sha, _, c := leaseFixture(t, "gate-inflight")
	canon := canonicalize(src)
	g := gitRunner(t)
	g(src, "branch", "dev", sha+"^")
	claim(t, c, "A", 0)
	claim(t, c, "B", 0)

	// Simulate the call being in flight, then run every deadline past.
	ageLease(t, canon, 3*time.Hour)
	st, _ := loadLease(canon)
	st.Holder.InFlight = 1
	st.Holder.InFlightSince = time.Now().UTC() // a live call: the mark is fresh
	_ = saveLease(canon, st)
	if err := TickLease(context.Background(), c, canon); err != nil {
		t.Fatal(err)
	}
	st, _ = loadLease(canon)
	if st.Holder == nil || st.Holder.TaskID != "A" {
		t.Fatalf("holder released mid-deploy: %+v", st.Holder)
	}
}

// ---- ship_feature recompose ----------------------------------------------------

// layeredShipFixture builds dogfood = main + F2 + F3 (independent files) with
// a fake argus that knows task-ship and task-holder. Returns the source repo,
// the fake, the client, the pre-ship dev SHA and the manifest path.
func layeredShipFixture(t *testing.T, slug string) (src string, f *fakeLeaseArgus, c *argus.Client, oldDev, manifestFile string) {
	t.Helper()
	src, wt, _, _ := setupRecomposeRepo(t, slug)
	g := gitRunner(t)
	mainSHA := revParse(t, src, "refs/heads/main")

	g(src, "checkout", "-b", "feature/F2", "main")
	writeRecomposeFile(t, src, "fileF2.txt", "f2\n")
	g(src, "add", "fileF2.txt")
	g(src, "commit", "-m", "F2")
	f2 := revParse(t, src, "refs/heads/feature/F2")
	g(src, "checkout", "-b", "feature/F3", "main")
	writeRecomposeFile(t, src, "fileF3.txt", "f3\n")
	g(src, "add", "fileF3.txt")
	g(src, "commit", "-m", "F3")
	f3 := revParse(t, src, "refs/heads/feature/F3")
	g(src, "checkout", "-b", "dev", "main")
	g(src, "cherry-pick", f2)
	g(src, "cherry-pick", f3)
	oldDev = revParse(t, src, "refs/heads/dev")
	g(src, "checkout", "main")

	stateDir, _ := SourceRepoStateDir(canonicalize(src))
	if err := WriteManifest(stateDir, &DogfoodManifest{
		Base:    ManifestBase{Ref: "main", SHA: mainSHA},
		Layered: []LayeredEntry{{Name: "feature/F2", SHA: f2, Applied: "cherry-pick"}, {Name: "feature/F3", SHA: f3, Applied: "cherry-pick"}},
	}); err != nil {
		t.Fatal(err)
	}
	manifestFile = filepath.Join(stateDir, DogfoodManifestFilename)
	writeFakeGH(t, fakeGHPRAutoBody(checksAllPass))

	f, c = newFakeLeaseArgus(t, src, wt)
	f.add("task-ship", "shipper")
	f.add("task-holder", "holder")
	return
}

func TestShipFeature_RecomposeSkippedWhileAnotherTaskHoldsLease(t *testing.T) {
	src, f, c, oldDev, manifestFile := layeredShipFixture(t, "ship-lease-skip")
	if _, err := DogfoodClaim(context.Background(), c, DogfoodClaimInput{TaskID: "task-holder"}); err != nil {
		t.Fatal(err)
	}
	beforeManifest, _ := os.ReadFile(manifestFile)

	res, err := ShipFeature(context.Background(), c, "task-ship", ShipFeatureOpts{Branch: "feature/F2", Via: "pr-auto"})
	if err != nil {
		t.Fatalf("ShipFeature: %v", err)
	}
	if !res.Shipped || !res.Merged {
		t.Fatalf("the ship itself must succeed: %+v", res)
	}
	if res.Recompose == nil || res.Recompose.Attempted || res.Recompose.Succeeded {
		t.Fatalf("recompose = %+v, want attempted=false succeeded=false", res.Recompose)
	}
	var w *Warning
	for i := range res.Warnings {
		if res.Warnings[i].Code == "recompose_skipped" {
			w = &res.Warnings[i]
		}
	}
	if w == nil || !strings.Contains(w.Message, "holder") || !strings.Contains(w.Message, "task-holder") {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
	if got := revParse(t, src, "refs/heads/dev"); got != oldDev {
		t.Fatal("dogfood branch must be unchanged")
	}
	if after, _ := os.ReadFile(manifestFile); string(after) != string(beforeManifest) {
		t.Fatal("manifest must be unchanged")
	}
	ns := f.notifiesTo("task-holder")
	if len(ns) == 0 || !strings.Contains(ns[len(ns)-1].Text, "feature/F2") || !strings.Contains(ns[len(ns)-1].Text, "recompose was skipped") {
		t.Fatalf("holder notice = %+v", ns)
	}
}

func TestShipFeature_RecomposeRunsWhenLeaseFree(t *testing.T) {
	src, _, c, oldDev, _ := layeredShipFixture(t, "ship-lease-free")
	res, err := ShipFeature(context.Background(), c, "task-ship", ShipFeatureOpts{Branch: "feature/F2", Via: "pr-auto"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Recompose == nil || !res.Recompose.Succeeded {
		t.Fatalf("recompose = %+v warnings=%+v", res.Recompose, res.Warnings)
	}
	if revParse(t, src, "refs/heads/dev") == oldDev {
		t.Fatal("dev should have been recomposed")
	}
	st, _ := loadLease(canonicalize(src))
	if st.Holder != nil {
		t.Fatalf("an internal recompose must not take the lease: %+v", st.Holder)
	}
}

func TestShipFeature_RecomposeByHolderCountsAsContact(t *testing.T) {
	src, _, c, _, _ := layeredShipFixture(t, "ship-lease-holder")
	canon := canonicalize(src)
	if _, err := DogfoodClaim(context.Background(), c, DogfoodClaimInput{TaskID: "task-ship"}); err != nil {
		t.Fatal(err)
	}
	ageLease(t, canon, 8*time.Minute)
	before, _ := loadLease(canon)
	res, err := ShipFeature(context.Background(), c, "task-ship", ShipFeatureOpts{Branch: "feature/F2", Via: "pr-auto"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Recompose == nil || !res.Recompose.Succeeded {
		t.Fatalf("recompose = %+v", res.Recompose)
	}
	after, _ := loadLease(canon)
	if after.Holder == nil || after.Holder.TaskID != "task-ship" || !after.Holder.LastContactAt.After(before.Holder.LastContactAt) || after.Holder.InFlight != 0 {
		t.Fatalf("holder = %+v", after.Holder)
	}
}

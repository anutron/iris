package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLeaseContent_ContainmentBeforeGrantAndHolderContact(t *testing.T) {
	for _, busy := range []bool{false, true} {
		h := newLeaseHarness(t)
		if busy {
			h.claim("A", 0)
		}
		h.take()
		h.now = h.now.Add(time.Second)
		m := h.m()
		m.env.Contains = func(s string) (bool, error) { return s == "S", nil }
		out := m.Claim("B", "b", 0, "S")
		if !out.AlreadyDeployed || out.Granted || out.SHA != "S" || len(h.st.Queue) != 0 || len(m.fx.Msgs) != 0 {
			t.Fatalf("out=%+v state=%+v effects=%+v", out, h.st, m.fx)
		}
		if busy {
			if h.holderID() != "A" || h.st.Holder.LastContactAt.Equal(h.now) {
				t.Fatal("claim contacted or replaced holder")
			}
			out = m.Claim("A", "a", 0, "S")
			if !out.Granted || out.AlreadyDeployed || !h.st.Holder.LastContactAt.Equal(h.now) {
				t.Fatalf("holder reclaim=%+v", out)
			}
		} else if h.st.Holder != nil {
			t.Fatal("unneeded lease granted")
		}
	}
}

func TestLeaseContent_UnknownContainmentAndNoticeInvalidation(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	m := h.m()
	m.Claim("B", "b", 0, "S")
	m.env.Contains = func(string) (bool, error) { return false, errors.New("git failed") }
	m.ReconcileQueue()
	h.wantQueue("B")
	m.env.Contains = func(string) (bool, error) { return true, nil }
	m.ReconcileQueue()
	if len(h.st.Notices) == 0 {
		t.Fatal("missing deployed notice")
	}
	m.fx = LeaseEffects{}
	m.env.Contains = func(string) (bool, error) { return false, errors.New("git failed") }
	m.CancelInvalidDeployments()
	if len(m.fx.Cancels) != 0 {
		t.Fatal("unknown containment cancelled notice")
	}
	m.env.Contains = func(string) (bool, error) { return false, nil }
	m.CancelInvalidDeployments()
	if len(m.fx.Cancels) != 1 || m.fx.Cancels[0].TaskID != "B" {
		t.Fatalf("invalidated notice not cancelled: %+v", m.fx)
	}
}

func TestDogfoodContent_WaitPinsInheritedRequestThroughRemoval(t *testing.T) {
	src, _, sha, f, c := leaseFixture(t, "content-wait")
	src = canonicalize(src)
	claim(t, c, "A", 0)
	if _, err := DogfoodClaim(t.Context(), c, DogfoodClaimInput{TaskID: "B", SHA: sha}); err != nil {
		t.Fatal(err)
	}
	_, lr, err := leaseTargetRepo(t.Context(), c, "A", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := withLease(t.Context(), c, lr, func(m *leaseMachine) { m.Notice("B", leaseMsgReleased, "old notice") }); err != nil {
		t.Fatal(err)
	}
	oldNotice := f.notifiesTo("B")[0].DeliveryID
	result := make(chan *DogfoodClaimResult, 1)
	errs := make(chan error, 1)
	go func() {
		r, err := DogfoodClaim(t.Context(), c, DogfoodClaimInput{TaskID: "B", WaitSeconds: 3})
		result <- r
		errs <- err
	}()
	// Cancellation proves the waiting call entered its lease transaction and
	// inherited the persisted SHA before deployment removes the queue entry.
	deadline := time.Now().Add(2 * time.Second)
	for {
		started := false
		f.with(func() {
			for _, cancel := range f.cancels {
				if cancel == "B/"+oldNotice {
					started = true
				}
			}
		})
		if started {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("waiting claim did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := SetDogfood(t.Context(), c, "A", SetDogfoodOpts{Sha: sha, Manifest: sampleManifest()}); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-result:
		if err := <-errs; err != nil || r.Status != "already_deployed" || r.SHA != sha {
			t.Fatalf("wait result=%+v err=%v", r, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait failed to finish on satisfaction")
	}
	if len(f.notifiesTo("B")) > 2 {
		t.Fatal("requeued or duplicate satisfaction")
	}
	st, err := loadLease(src)
	if err != nil || len(st.Queue) != 0 || st.Holder.TaskID != "A" {
		t.Fatalf("state=%+v err=%v", st, err)
	}
}

func TestDogfoodContent_FailedReloadAndRollbackCancellation(t *testing.T) {
	src, wt, sha, f, c := leaseFixture(t, "content-failed")
	src = canonicalize(src)
	g := gitRunner(t)
	claim(t, c, "A", 0)
	_, err := DogfoodClaim(t.Context(), c, DogfoodClaimInput{TaskID: "B", SHA: sha})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetDogfood(t.Context(), c, "A", SetDogfoodOpts{Sha: sha, Manifest: sampleManifest()}); err != nil {
		t.Fatal(err)
	}
	notices := f.notifiesTo("B")
	if len(notices) != 1 {
		t.Fatalf("notices=%+v", notices)
	}
	badConfig := strings.Replace(tomlDogfoodNone, `command = ["true"]`, `command = ["false"]`, 1)
	if err := os.WriteFile(filepath.Join(wt, ".iris.toml"), []byte(badConfig), 0600); err != nil {
		t.Fatal(err)
	}
	g(wt, "add", ".iris.toml")
	g(wt, "commit", "-m", "failing build")
	bad := strings.TrimSpace(g(wt, "rev-parse", "HEAD"))
	_, err = DogfoodClaim(t.Context(), c, DogfoodClaimInput{TaskID: "C", SHA: bad})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetDogfood(t.Context(), c, "A", SetDogfoodOpts{Sha: bad, Manifest: sampleManifest()}); err == nil {
		t.Fatal("expected failed reload")
	}
	st, err := loadLease(src)
	if err != nil || len(st.Queue) != 1 || st.Queue[0].TaskID != "C" || len(f.notifiesTo("C")) != 0 {
		t.Fatalf("failed reload satisfied waiter: %+v err=%v", st, err)
	}
	// Force an unrelated tree with a failing build. Cancellation must precede
	// reload and survive its failure, without automatically requeuing B.
	unrelated := strings.TrimSpace(g(src, "commit-tree", bad+"^{tree}", "-m", "unrelated"))
	if _, err := SetDogfood(t.Context(), c, "A", SetDogfoodOpts{Sha: unrelated, Manifest: sampleManifest(), Force: true}); err == nil {
		t.Fatal("expected rollback reload failure")
	}
	found := false
	f.with(func() {
		for _, cancel := range f.cancels {
			if cancel == "B/"+notices[0].DeliveryID {
				found = true
			}
		}
	})
	if !found {
		t.Fatal("failed rollback did not cancel success notice")
	}
	st, err = loadLease(src)
	if err != nil || len(st.Queue) != 1 || st.Queue[0].TaskID != "C" {
		t.Fatalf("unexpected requeue: %+v %v", st, err)
	}
}

func TestDogfoodContent_OperationalGitErrorRetainsQueue(t *testing.T) {
	src, _, sha, _, c := leaseFixture(t, "content-git-error")
	src = canonicalize(src)
	claim(t, c, "A", 0)
	if _, err := DogfoodClaim(t.Context(), c, DogfoodClaimInput{TaskID: "B", SHA: sha}); err != nil {
		t.Fatal(err)
	}
	// Preserve a syntactically valid but missing object ID in the branch ref.
	if err := os.WriteFile(filepath.Join(src, ".git", "refs", "heads", "dev"), []byte(strings.Repeat("1", 40)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, lr, err := leaseTargetRepo(t.Context(), c, "A", "")
	if err != nil {
		t.Fatal(err)
	}
	warnings, err := withLease(t.Context(), c, lr, func(m *leaseMachine) { m.ReconcileQueue() })
	if err != nil || len(warnings) == 0 {
		t.Fatalf("warnings=%v err=%v", warnings, err)
	}
	st, err := loadLease(src)
	if err != nil || len(st.Queue) != 1 {
		t.Fatalf("state=%+v err=%v", st, err)
	}
}

func TestLeaseContent_ReconcileAllAndHandoff(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.take()
	m := h.m()
	m.Claim("B", "b", 0, "S")
	m.Claim("C", "c", 0, "T")
	m.Claim("D", "d", 0, "R")
	m.Claim("legacy", "legacy", 0)
	m.env.Contains = func(s string) (bool, error) { return s == "S" || s == "R", nil }
	m.fx = LeaseEffects{}
	m.ReconcileQueue()
	h.wantQueue("C", "legacy")
	if h.holderID() != "A" || !eqStrs(kindsOf(m.fx.Msgs), []string{"deployed:B", "deployed:D"}) {
		t.Fatalf("state=%+v effects=%+v", h.st, m.fx)
	}
	for _, msg := range m.fx.Msgs {
		for _, want := range []string{"[iris]", "dev", "now live", "--force", "iris_status", "lease #"} {
			if !strings.Contains(msg.Text, want) {
				t.Errorf("notice missing %q: %s", want, msg.Text)
			}
		}
	}
	m.fx = LeaseEffects{}
	m.Release("A")
	if h.holderID() != "C" || !eqStrs(kindsOf(m.fx.Msgs), []string{"grant:C"}) {
		t.Fatalf("handoff=%+v", m.fx)
	}
}

func TestLeaseContent_PreHandoffAndLastWaiter(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	m := h.m()
	m.Claim("B", "b", 0, "S")
	h.st.Holder.ReleaseAt = h.now.Add(time.Second)
	h.st.Holder.FinalWarningAt = h.now
	m.Notice("A", leaseMsgFinal, "warning")
	m.fx = LeaseEffects{}
	m.env.Contains = func(s string) (bool, error) { return true, nil }
	m.ReconcileQueue()
	if !h.st.Holder.ReleaseAt.IsZero() || !h.st.Holder.FinalWarningAt.IsZero() || len(m.fx.Cancels) == 0 {
		t.Fatalf("holder=%+v effects=%+v", h.st.Holder, m.fx)
	}
	m.Release("A")
	if h.st.Holder != nil {
		t.Fatal("satisfied waiter was granted")
	}
	// Handoff itself performs reconciliation, including automatic evictions.
	h = newLeaseHarness(t)
	h.claim("A", 0)
	m = h.m()
	m.Claim("B", "b", 0, "S")
	m.env.Contains = func(string) (bool, error) { return true, nil }
	m.ForceRelease("test")
	if h.st.Holder != nil || len(h.st.Queue) != 0 {
		t.Fatal("handoff failed to satisfy")
	}
}

func TestLeaseContent_ReclaimRequestAndNoticeLifecycle(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	m := h.m()
	m.Claim("B", "b", 0, "S")
	if out := m.Claim("B", "b", 0); out.SHA != "S" {
		t.Fatalf("lost request: %+v", out)
	}
	if out := m.Claim("B", "b", 0, "T"); out.SHA != "T" || h.st.Queue[0].SHA != "T" {
		t.Fatalf("replacement: %+v", out)
	}
	m.env.Contains = func(s string) (bool, error) { return s == "T", nil }
	m.ReconcileQueue()
	body, err := marshalLease(h.st)
	if err != nil {
		t.Fatal(err)
	}
	var st LeaseState
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatal(err)
	}
	m = newLeaseMachine(&st, m.env)
	m.fx = LeaseEffects{}
	m.Release("B")
	if len(m.fx.Cancels) != 1 || m.fx.Cancels[0].TaskID != "B" {
		t.Fatalf("release did not cancel persisted notice: %+v", m.fx)
	}
}

func TestDogfoodContent_ClaimsResolveAndPreserveLegacy(t *testing.T) {
	src, _, sha, f, c := leaseFixture(t, "content-claim")
	src = canonicalize(src)
	g := gitRunner(t)
	g(src, "branch", "dev", sha)
	g(src, "branch", "wanted", sha+"^")
	ctx := context.Background()
	r, err := DogfoodClaim(ctx, c, DogfoodClaimInput{TaskID: "B", SHA: "wanted"})
	if err != nil {
		t.Fatal(err)
	}
	ancestor := strings.TrimSpace(g(src, "rev-parse", "wanted"))
	if r.Status != "already_deployed" || r.SHA != ancestor || r.Holder != nil || r.NextCheckinAt != nil || r.Position != 0 || len(r.Ahead) != 0 || len(f.notifiesTo("B")) != 0 {
		t.Fatalf("result=%+v", r)
	}
	claim(t, c, "A", 0)
	r, err = DogfoodClaim(ctx, c, DogfoodClaimInput{TaskID: "B", SHA: sha})
	if err != nil || r.Status != "already_deployed" || r.Holder.TaskID != "A" {
		t.Fatalf("busy=%+v %v", r, err)
	}
	if r := claim(t, c, "C", 0); r.Status != "queued" {
		t.Fatalf("legacy=%+v", r)
	}
	before, _ := loadLease(src)
	data, _ := marshalLease(before)
	for _, bad := range []string{"--help", "missing-commit"} {
		if _, err := DogfoodClaim(ctx, c, DogfoodClaimInput{TaskID: "B", SHA: bad}); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	after, _ := loadLease(src)
	data2, _ := marshalLease(after)
	if string(data) != string(data2) {
		t.Fatal("invalid SHA changed state")
	}
}

func TestDogfoodContent_DeploySatisfiesAndReleaseDoesNotDuplicate(t *testing.T) {
	src, _, sha, f, c := leaseFixture(t, "content-deploy")
	src = canonicalize(src)
	claim(t, c, "A", 0)
	r, err := DogfoodClaim(t.Context(), c, DogfoodClaimInput{TaskID: "B", SHA: sha})
	if err != nil || r.Status != "queued" {
		t.Fatalf("claim=%+v %v", r, err)
	}
	claim(t, c, "C", 0)
	if _, err := SetDogfood(t.Context(), c, "A", SetDogfoodOpts{Sha: sha, Manifest: sampleManifest()}); err != nil {
		t.Fatal(err)
	}
	st, _ := loadLease(src)
	if st.Holder.TaskID != "A" || len(st.Queue) != 1 || st.Queue[0].TaskID != "C" {
		t.Fatalf("state=%+v", st)
	}
	notices := f.notifiesTo("B")
	if len(notices) != 1 || !strings.Contains(notices[0].Text, "now live") {
		t.Fatalf("notices=%+v", notices)
	}
	if _, err := DogfoodRelease(t.Context(), c, "A"); err != nil {
		t.Fatal(err)
	}
	if len(f.notifiesTo("B")) != 1 {
		t.Fatal("duplicate notice")
	}
	if _, err := DogfoodRelease(t.Context(), c, "B"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	found := false
	for _, s := range f.cancels {
		if s == "B/"+notices[0].DeliveryID {
			found = true
		}
	}
	if !found {
		t.Fatal("satisfied release did not cancel notice")
	}
}

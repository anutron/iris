package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anutron/iris/internal/argus"
)

// fakeLeaseArgus is an argus stand-in with per-task name/status/bridge state,
// notify capture, and failure injection.
type fakeLeaseArgus struct {
	mu         sync.Mutex
	tasks      map[string]*fakeLeaseTask
	notifies   []fakeNotify
	cancels    []string // "task/delivery"
	notifyHTTP int      // when >0, notify returns this status
	worktree   string
	src        string
	srv        *httptest.Server
}

type fakeLeaseTask struct {
	Name     string
	Status   string
	Archived bool
	Bridge   string // idle | busy | needs_input | dead | none
}

type fakeNotify struct {
	Task, Text, DeliveryID string
	DeadlineMS             float64
}

func newFakeLeaseArgus(t *testing.T, src, wt string) (*fakeLeaseArgus, *argus.Client) {
	t.Helper()
	canon, _ := filepath.EvalSymlinks(src)
	f := &fakeLeaseArgus{tasks: map[string]*fakeLeaseTask{}, worktree: wt, src: canon}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f, argus.New(f.srv.URL, "stub-token")
}

func (f *fakeLeaseArgus) add(id, name string) *fakeLeaseTask {
	f.mu.Lock()
	defer f.mu.Unlock()
	tk := &fakeLeaseTask{Name: name, Status: "in_progress", Bridge: "idle"}
	f.tasks[id] = tk
	return tk
}

func (f *fakeLeaseArgus) with(fn func()) { f.mu.Lock(); defer f.mu.Unlock(); fn() }

func (f *fakeLeaseArgus) notifiesTo(id string) []fakeNotify {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeNotify
	for _, n := range f.notifies {
		if n.Task == id {
			out = append(out, n)
		}
	}
	return out
}

func (f *fakeLeaseArgus) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/api/projects/full" {
		_ = json.NewEncoder(w).Encode(map[string]any{"projects": []map[string]any{{"name": "iris-test", "path": f.src}}})
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	if rest == r.URL.Path {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(rest, "/")
	id := parts[0]
	tk, ok := f.tasks[id]
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch {
	case len(parts) == 1:
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": id, "name": tk.Name, "project": "iris-test", "status": tk.Status,
			"archived": tk.Archived, "worktree_path": f.worktree,
		})
	case len(parts) == 2 && parts[1] == "bridge":
		switch tk.Bridge {
		case "none":
			http.Error(w, "no bridge state for task", http.StatusNotFound)
		case "dead":
			_ = json.NewEncoder(w).Encode(map[string]any{"live": false})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"live": true, "busy": tk.Bridge == "busy", "needs_input": tk.Bridge == "needs_input"})
		}
	case len(parts) == 2 && parts[1] == "notify" && r.Method == "POST":
		if f.notifyHTTP > 0 {
			http.Error(w, "unavailable", f.notifyHTTP)
			return
		}
		var body struct {
			Text       string  `json:"text"`
			DeliveryID string  `json:"delivery_id"`
			DeadlineMS float64 `json:"deadline_ms"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.notifies = append(f.notifies, fakeNotify{Task: id, Text: body.Text, DeliveryID: body.DeliveryID, DeadlineMS: body.DeadlineMS})
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{}`))
	case len(parts) == 3 && parts[1] == "notify" && r.Method == "DELETE":
		f.cancels = append(f.cancels, id+"/"+parts[2])
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

// leaseFixture: a dogfood repo with a fake argus and tasks A, B, C, D.
func leaseFixture(t *testing.T, slug string) (src, wt, sha string, f *fakeLeaseArgus, client *argus.Client) {
	t.Helper()
	// Not setupDogfoodRepo: the shared fixture pre-claims for task-sd.
	setAuditDir(t)
	src, wt, _ = setupRepoWithBareAndWorktree(t, slug)
	g := gitRunner(t)
	for _, dir := range []string{src, wt} {
		if err := os.WriteFile(filepath.Join(dir, ".iris.toml"), []byte(tomlDogfoodNone), 0o644); err != nil {
			t.Fatal(err)
		}
		g(dir, "add", ".iris.toml")
		g(dir, "commit", "-m", "fixture files")
	}
	sha = strings.TrimSpace(g(wt, "rev-parse", "HEAD"))
	f, client = newFakeLeaseArgus(t, src, wt)
	for _, id := range []string{"A", "B", "C", "D"} {
		f.add(id, "task-"+strings.ToLower(id))
	}
	return
}

func claim(t *testing.T, c *argus.Client, id string, pos int) *DogfoodClaimResult {
	t.Helper()
	r, err := DogfoodClaim(context.Background(), c, DogfoodClaimInput{TaskID: id, Position: pos})
	if err != nil {
		t.Fatalf("claim %s: %v", id, err)
	}
	return r
}

// ---- claim ---------------------------------------------------------------

func TestDogfoodClaim_GrantedThenQueued(t *testing.T) {
	_, _, _, f, c := leaseFixture(t, "lv-claim")
	r := claim(t, c, "A", 0)
	if r.Status != "granted" || r.Position != 0 || r.DogfoodBranch != "dev" || r.NextCheckinAt == nil || len(r.Ahead) != 0 {
		t.Fatalf("granted result = %+v", r)
	}
	ns := f.notifiesTo("A")
	if len(ns) != 1 || !strings.HasPrefix(ns[0].Text, "[iris]") || !strings.Contains(ns[0].Text, "iris_dogfood_release") || ns[0].DeadlineMS != 3600000 {
		t.Fatalf("grant notify = %+v", ns)
	}

	r = claim(t, c, "B", 0)
	if r.Status != "queued" || r.Position != 1 || r.Holder == nil || r.Holder.TaskID != "A" || r.Holder.TaskName != "task-a" {
		t.Fatalf("queued result = %+v", r)
	}
	if !strings.Contains(r.Message, "task-a") || !strings.Contains(r.Message, "position 1") || !strings.Contains(r.Message, "message you") {
		t.Fatalf("message = %q", r.Message)
	}
	ns = f.notifiesTo("A")
	if len(ns) != 2 || !strings.Contains(ns[1].Text, "1 agent(s) waiting") || ns[1].DeadlineMS != 600000 {
		t.Fatalf("check-in notify = %+v", ns)
	}

	r = claim(t, c, "C", 0)
	d := claim(t, c, "D", 0)
	if d.Position != 3 || len(d.Ahead) != 2 || d.Ahead[0].TaskID != "B" || d.Ahead[1].TaskName != "task-c" {
		t.Fatalf("D result = %+v (C=%+v)", d, r)
	}
	if got := len(f.notifiesTo("A")); got != 2 {
		t.Fatalf("a check-in is already outstanding; joins must not add more (A got %d)", got)
	}
}

func TestDogfoodClaim_WaitReturnsWhenGranted(t *testing.T) {
	_, _, _, _, c := leaseFixture(t, "lv-wait")
	old := leasePollInterval
	leasePollInterval = 20 * time.Millisecond
	t.Cleanup(func() { leasePollInterval = old })
	claim(t, c, "A", 0)
	go func() {
		time.Sleep(150 * time.Millisecond)
		_, _ = DogfoodRelease(context.Background(), c, "A")
	}()
	start := time.Now()
	r, err := DogfoodClaim(context.Background(), c, DogfoodClaimInput{TaskID: "B", WaitSeconds: 20})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "granted" || time.Since(start) > 10*time.Second {
		t.Fatalf("r=%+v after %v", r, time.Since(start))
	}
}

func TestDogfoodClaim_WaitTimesOutQueued(t *testing.T) {
	_, _, _, _, c := leaseFixture(t, "lv-waitq")
	old := leasePollInterval
	leasePollInterval = 10 * time.Millisecond
	t.Cleanup(func() { leasePollInterval = old })
	claim(t, c, "A", 0)
	// 0 seconds of waiting returns at once.
	r, err := DogfoodClaim(context.Background(), c, DogfoodClaimInput{TaskID: "B", WaitSeconds: 0})
	if err != nil || r.Status != "queued" {
		t.Fatalf("r=%+v err=%v", r, err)
	}
}

func TestClampLeaseWait(t *testing.T) {
	for in, want := range map[int]int{-5: 0, 0: 0, 7: 7, 25: 25, 26: 25, 120: 25} {
		if got := clampLeaseWait(in); got != want {
			t.Errorf("clampLeaseWait(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestDogfoodClaim_RequiresTaskAndDogfoodBranch(t *testing.T) {
	_, _, _, _, c := leaseFixture(t, "lv-req")
	if _, err := DogfoodClaim(context.Background(), c, DogfoodClaimInput{}); err == nil {
		t.Fatal("task_id required")
	}
	const noDogfood = "schema_version = 1\n[build]\ncommand = [\"true\"]\n[restart]\nmechanism = \"none\"\n"
	src, wt, _ := setupRepoWithBareAndWorktree(t, "lv-nodf")
	if err := os.WriteFile(filepath.Join(src, ".iris.toml"), []byte(noDogfood), 0o644); err != nil {
		t.Fatal(err)
	}
	f, c2 := newFakeLeaseArgus(t, src, wt)
	f.add("A", "a")
	_, err := DogfoodClaim(context.Background(), c2, DogfoodClaimInput{TaskID: "A"})
	if err == nil || !strings.Contains(err.Error(), "dogfood_branch not configured") {
		t.Fatalf("err = %v", err)
	}
	if leaseFileExists(canonicalize(src)) {
		t.Fatal("no lease state may be written")
	}
}

func TestDogfoodClaim_DeadHolderGrantedOnTheSpot(t *testing.T) {
	_, _, _, f, c := leaseFixture(t, "lv-dead")
	claim(t, c, "A", 0)
	f.with(func() { f.tasks["A"].Status = "complete" })
	r := claim(t, c, "B", 0)
	if r.Status != "granted" {
		t.Fatalf("r = %+v", r)
	}
	ns := f.notifiesTo("A")
	if len(ns) == 0 || !strings.Contains(ns[len(ns)-1].Text, "Released your dogfood lease") {
		t.Fatalf("A should be told: %+v", ns)
	}
}

// ---- renew / release / force / move ---------------------------------------

func TestDogfoodRenew(t *testing.T) {
	_, _, _, f, c := leaseFixture(t, "lv-renew")
	claim(t, c, "A", 0)
	claim(t, c, "B", 0) // check-in goes out to A
	res, err := DogfoodRenew(context.Background(), c, "A")
	if err != nil || !res.Renewed || res.NextCheckinAt.Before(time.Now().Add(9*time.Minute)) {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	f.with(func() {
		if len(f.cancels) != 1 || !strings.HasPrefix(f.cancels[0], "A/") {
			t.Fatalf("pending check-in should be cancelled: %v", f.cancels)
		}
	})
	_, err = DogfoodRenew(context.Background(), c, "B")
	var ref *LeaseRefusal
	if !errors.As(err, &ref) || ref.Holder.TaskID != "A" || ref.Position != 1 {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "task-a") {
		t.Fatalf("error should name the holder: %v", err)
	}
}

func TestDogfoodRelease(t *testing.T) {
	_, _, _, f, c := leaseFixture(t, "lv-release")
	claim(t, c, "A", 0)
	claim(t, c, "B", 0)
	r, err := DogfoodRelease(context.Background(), c, "A")
	if err != nil || !r.Released || r.Was != "holder" || r.GrantedTo != "B" {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	ns := f.notifiesTo("B")
	if len(ns) != 1 || !strings.Contains(ns[0].Text, "You have the dogfood lease") {
		t.Fatalf("B grant = %+v", ns)
	}
	r, _ = DogfoodRelease(context.Background(), c, "C")
	if r.Released || r.Was != "none" {
		t.Fatalf("r = %+v", r)
	}
}

func TestDogfoodForceRelease(t *testing.T) {
	src, _, _, f, c := leaseFixture(t, "lv-force")
	claim(t, c, "A", 0)
	claim(t, c, "B", 0)
	f.with(func() { f.tasks["A"].Bridge = "busy" })
	if _, err := DogfoodForceRelease(context.Background(), c, DogfoodForceReleaseInput{TaskID: "B"}); err == nil {
		t.Fatal("reason required")
	}
	st, _ := loadLease(canonicalize(src))
	if st.Holder == nil || st.Holder.TaskID != "A" {
		t.Fatal("no state change without a reason")
	}
	r, err := DogfoodForceRelease(context.Background(), c, DogfoodForceReleaseInput{TaskID: "B", Reason: "A is looping"})
	if err != nil || !r.Released || r.GrantedTo != "B" || r.Evicted != "A" {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	ns := f.notifiesTo("A")
	if !strings.Contains(ns[len(ns)-1].Text, "A is looping") {
		t.Fatalf("A not told the reason: %+v", ns)
	}
}

func TestDogfoodForceRelease_ArgusUnreachableStillReleases(t *testing.T) {
	src, wt, _, f, c := leaseFixture(t, "lv-force-down")
	claim(t, c, "A", 0)
	claim(t, c, "B", 0)
	// iris-on-iris style: no task/path, so resolution needs no argus.
	bin := filepath.Join(src, "bin", "iris")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	_ = os.WriteFile(bin, []byte("x"), 0o755)
	old := executable
	executable = func() (string, error) { return bin, nil }
	t.Cleanup(func() { executable = old })
	_ = wt

	f.srv.Close() // argus is now unreachable
	r, err := DogfoodForceRelease(context.Background(), c, DogfoodForceReleaseInput{Reason: "argus is down"})
	if err != nil {
		t.Fatalf("force-release must work without argus: %v", err)
	}
	if !r.Released || r.GrantedTo != "B" || len(r.Warnings) == 0 {
		t.Fatalf("r = %+v (want released, B granted, a delivery warning)", r)
	}
	st, _ := loadLease(canonicalize(src))
	if st.Holder == nil || st.Holder.TaskID != "B" {
		t.Fatalf("holder = %+v", st.Holder)
	}
}

func TestDogfoodQueueMove(t *testing.T) {
	_, _, _, f, c := leaseFixture(t, "lv-move")
	claim(t, c, "A", 0)
	claim(t, c, "B", 0)
	claim(t, c, "C", 0)
	claim(t, c, "D", 0)
	before := len(f.notifies)
	r, err := DogfoodQueueMove(context.Background(), c, DogfoodQueueMoveInput{TaskID: "A", MoveTaskID: "D", Position: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Queue) != 3 || r.Queue[0].TaskID != "D" || r.Queue[1].TaskID != "B" || r.Queue[2].TaskID != "C" {
		t.Fatalf("queue = %+v", r.Queue)
	}
	if len(f.notifies) != before {
		t.Fatal("queue-move must not message anyone")
	}
	if _, err := DogfoodQueueMove(context.Background(), c, DogfoodQueueMoveInput{TaskID: "A", MoveTaskID: "A", Position: 1}); err == nil || !strings.Contains(err.Error(), "holds the lease") {
		t.Fatalf("err = %v", err)
	}
}

// ---- scheduler tick + persistence -----------------------------------------

// ageLease rewrites the persisted holder's clocks backwards.
func ageLease(t *testing.T, src string, by time.Duration) {
	t.Helper()
	st, err := loadLease(src)
	if err != nil || st.Holder == nil {
		t.Fatalf("load: %v holder=%v", err, st.Holder)
	}
	h := st.Holder
	h.GrantedAt, h.LastContactAt = h.GrantedAt.Add(-by), h.LastContactAt.Add(-by)
	for _, p := range []*time.Time{&h.CheckinSentAt, &h.FinalWarningAt, &h.ReleaseAt} {
		if !p.IsZero() {
			*p = p.Add(-by)
		}
	}
	if err := saveLease(src, st); err != nil {
		t.Fatal(err)
	}
}

func TestTickLease_WarmerCheckinDelivered(t *testing.T) {
	src, _, _, f, c := leaseFixture(t, "lv-tick")
	canon := canonicalize(src)
	claim(t, c, "A", 0)
	ageLease(t, canon, 11*time.Minute)
	if err := TickLease(context.Background(), c, canon); err != nil {
		t.Fatal(err)
	}
	ns := f.notifiesTo("A")
	if len(ns) != 2 || !strings.Contains(ns[1].Text, "Still using dogfood") {
		t.Fatalf("notifies = %+v", ns)
	}
	st, _ := loadLease(canon)
	if st.Holder.CheckinSentAt.IsZero() {
		t.Fatal("check-in should be recorded")
	}
}

func TestTickLease_UndeliveredCheckinIsRetriedNotSilence(t *testing.T) {
	src, _, _, f, c := leaseFixture(t, "lv-undeliv")
	canon := canonicalize(src)
	claim(t, c, "A", 0)
	f.with(func() { f.notifyHTTP = 503 })
	claim(t, c, "B", 0) // wants an immediate check-in; delivery fails
	ageLease(t, canon, 5*time.Minute)
	_ = TickLease(context.Background(), c, canon)
	st, _ := loadLease(canon)
	if st.Holder.TaskID != "A" || !st.Holder.CheckinSentAt.IsZero() || !st.Holder.CheckinWanted {
		t.Fatalf("an undelivered check-in must not count as silence: %+v", st.Holder)
	}
	view := ReadDogfoodLeaseView(context.Background(), canon)
	joined := strings.Join(view.Warnings, "\n")
	if !strings.Contains(joined, "could not deliver") {
		t.Fatalf("status should show the delivery error: %v", view.Warnings)
	}
	f.with(func() { f.notifyHTTP = 0 })
	_ = TickLease(context.Background(), c, canon)
	st, _ = loadLease(canon)
	if st.Holder.CheckinSentAt.IsZero() {
		t.Fatal("check-in should have been retried and delivered")
	}
}

func TestTickLease_ArgusUnreachableNeverReleasesOnSilence(t *testing.T) {
	src, _, _, f, c := leaseFixture(t, "lv-unreach")
	canon := canonicalize(src)
	claim(t, c, "A", 0)
	claim(t, c, "B", 0)
	ageLease(t, canon, 2*time.Minute) // way past the reply window
	f.srv.Close()
	_ = TickLease(context.Background(), c, canon)
	st, _ := loadLease(canon)
	if st.Holder == nil || st.Holder.TaskID != "A" {
		t.Fatalf("holder = %+v; must not release on silence it cannot verify", st.Holder)
	}
	view := ReadDogfoodLeaseView(context.Background(), canon)
	if !strings.Contains(strings.Join(view.Warnings, "\n"), "liveness lookup") {
		t.Fatalf("status should show the lookup error: %v", view.Warnings)
	}
}

func TestTickLease_SilenceHandsOverEndToEnd(t *testing.T) {
	src, _, _, f, c := leaseFixture(t, "lv-e2e")
	canon := canonicalize(src)
	claim(t, c, "A", 0)
	claim(t, c, "B", 0) // check-in sent to A
	ageLease(t, canon, 40*time.Second)
	_ = TickLease(context.Background(), c, canon) // → final warning
	if ns := f.notifiesTo("A"); !strings.Contains(ns[len(ns)-1].Text, "releasing the dogfood lease") {
		t.Fatalf("expected final warning, got %+v", ns)
	}
	ageLease(t, canon, 40*time.Second)
	_ = TickLease(context.Background(), c, canon) // → release + grant
	st, _ := loadLease(canon)
	if st.Holder == nil || st.Holder.TaskID != "B" || len(st.Queue) != 0 {
		t.Fatalf("state = %+v", st)
	}
	if ns := f.notifiesTo("B"); len(ns) == 0 || !strings.Contains(ns[len(ns)-1].Text, "You have the dogfood lease") {
		t.Fatalf("B should be told: %+v", ns)
	}
}

func TestActiveLeaseRepos_ListsOnlyHeldOrQueued(t *testing.T) {
	src, _, _, _, c := leaseFixture(t, "lv-active")
	canon := canonicalize(src)
	if got := ActiveLeaseRepos(); len(got) != 0 {
		t.Fatalf("nothing claimed yet: %v", got)
	}
	claim(t, c, "A", 0)
	if got := ActiveLeaseRepos(); len(got) != 1 || got[0] != canon {
		t.Fatalf("held lease must be discovered (even one made by another process): %v", got)
	}
	_, _ = DogfoodRelease(context.Background(), c, "A")
	if got := ActiveLeaseRepos(); len(got) != 0 {
		t.Fatalf("a free lease drops out: %v", got)
	}
}

func TestLockLease_SerializesAcrossHolders(t *testing.T) {
	setAuditDir(t)
	src := t.TempDir()
	unlock := lockLease(src)
	got := make(chan struct{})
	go func() { u := lockLease(src); u(); close(got) }()
	select {
	case <-got:
		t.Fatal("second lock acquired while the first was held")
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("second lock never acquired after unlock")
	}
}

func TestStartupScanLeases_RestartDoesNotReleaseAndKeepsContact(t *testing.T) {
	src, _, _, _, c := leaseFixture(t, "lv-scan")
	canon := canonicalize(src)
	claim(t, c, "A", 0)
	claim(t, c, "B", 0)
	ageLease(t, canon, 40*time.Second) // reply window long gone
	st, _ := loadLease(canon)
	contact := st.Holder.LastContactAt

	startup := time.Now().UTC()
	StartupScanLeases(context.Background(), startup)
	st, _ = loadLease(canon)
	if !st.Holder.LastContactAt.Equal(contact) {
		t.Fatal("last_contact_at must not change")
	}
	if st.Holder.CheckinSentAt.Before(startup) {
		t.Fatalf("reply window should restart at startup: %v < %v", st.Holder.CheckinSentAt, startup)
	}
	_ = TickLease(context.Background(), c, canon)
	st, _ = loadLease(canon)
	if st.Holder.TaskID != "A" || !st.Holder.ReleaseAt.IsZero() {
		t.Fatalf("restart must not count as silence: %+v", st.Holder)
	}
}

func TestRunLeaseScheduler_StopsOnCancel(t *testing.T) {
	_, _, _, _, c := leaseFixture(t, "lv-sched")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { RunLeaseScheduler(ctx, c, 10*time.Millisecond); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not stop on shutdown")
	}
}

func TestLeaseFile_AtomicRoundTrip(t *testing.T) {
	setAuditDir(t)
	src := t.TempDir()
	st := &LeaseState{Holder: &LeaseHolder{TaskID: "A", TaskName: "a", LastContactAt: time.Now().UTC()}, Queue: []LeaseEntry{{TaskID: "B"}}}
	if err := saveLease(src, st); err != nil {
		t.Fatal(err)
	}
	got, err := loadLease(src)
	if err != nil || got.Holder.TaskID != "A" || len(got.Queue) != 1 || got.SourceRepo != src {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	p, _ := leasePath(src)
	if m, _ := filepath.Glob(p + ".*.tmp"); len(m) != 0 {
		t.Fatalf("temp files left behind: %v", m)
	}
}

func TestLeaseHistoryCappedAt20(t *testing.T) {
	h := newLeaseHarness(t)
	for i := 0; i < 30; i++ {
		h.claim("A", 0)
		h.run(func(m *leaseMachine) { m.Release("A") })
	}
	if len(h.st.History) != 20 {
		t.Fatalf("history len = %d, want 20", len(h.st.History))
	}
}

// ---- status view ------------------------------------------------------------

func TestReadDogfoodLeaseView(t *testing.T) {
	src, _, _, _, c := leaseFixture(t, "lv-status")
	canon := canonicalize(src)
	v := ReadDogfoodLeaseView(context.Background(), canon)
	if v == nil || v.Holder != nil || len(v.Queue) != 0 || v.Timers.WarmerSeconds != 600 || v.Timers.GraceSeconds != 30 || v.Timers.IdleReleaseSeconds != 1800 || v.Timers.IdleReleaseAt != nil {
		t.Fatalf("free view = %+v", v)
	}
	claim(t, c, "A", 0)
	claim(t, c, "B", 0)
	v = ReadDogfoodLeaseView(context.Background(), canon)
	if v.Holder == nil || v.Holder.TaskID != "A" || v.Holder.TaskName != "task-a" || v.Holder.NextCheckinAt.IsZero() || v.Holder.CheckinOutstandingSince == nil {
		t.Fatalf("holder view = %+v", v.Holder)
	}
	if len(v.Queue) != 1 || v.Queue[0].TaskID != "B" || v.Queue[0].Position != 1 || v.Timers.IdleReleaseAt == nil || len(v.History) == 0 {
		t.Fatalf("view = %+v", v)
	}
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), `"in_flight_set_dogfood":false`) {
		t.Fatalf("json = %s", b)
	}
}

func TestReadDogfoodLeaseView_MalformedFileTolerated(t *testing.T) {
	src, _, _, _, c := leaseFixture(t, "lv-bad")
	canon := canonicalize(src)
	claim(t, c, "A", 0)
	p, _ := leasePath(canon)
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := ReadDogfoodLeaseView(context.Background(), canon)
	if v == nil || v.Holder != nil || len(v.Queue) != 0 || len(v.Warnings) == 0 || !strings.Contains(v.Warnings[0], LeaseFilename) {
		t.Fatalf("view = %+v", v)
	}
}

func TestReadDogfoodLeaseView_NilWithoutDogfoodBranch(t *testing.T) {
	src, _, _ := setupRepoWithBareAndWorktree(t, "lv-nodf-status")
	setAuditDir(t)
	if v := ReadDogfoodLeaseView(context.Background(), canonicalize(src)); v != nil {
		t.Fatalf("view = %+v, want nil", v)
	}
}

func TestReadDogfoodLeaseView_HasNoSideEffects(t *testing.T) {
	src, _, _, f, c := leaseFixture(t, "lv-ro")
	canon := canonicalize(src)
	claim(t, c, "A", 0)
	ageLease(t, canon, 2*time.Hour) // overdue everything
	p, _ := leasePath(canon)
	before, _ := os.ReadFile(p)
	n := len(f.notifies)
	_ = ReadDogfoodLeaseView(context.Background(), canon)
	after, _ := os.ReadFile(p)
	if string(before) != string(after) || len(f.notifies) != n {
		t.Fatal("status must not message, release or rewrite the lease")
	}
}

func TestReadDogfoodLeaseView_InvalidTimerConfigWarnsAndUsesDefaults(t *testing.T) {
	src, _, _, _, _ := leaseFixture(t, "lv-badcfg")
	if err := os.WriteFile(filepath.Join(src, ".iris.local.toml"), []byte("[dogfood_lease]\ngrace_seconds = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := ReadDogfoodLeaseView(context.Background(), canonicalize(src))
	if v.Timers.GraceSeconds != 30 || len(v.Warnings) == 0 || !strings.Contains(v.Warnings[0], "dogfood_lease") {
		t.Fatalf("view = %+v", v)
	}
}

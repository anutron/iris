package verbs

import (
	"strings"
	"testing"
	"time"

	"github.com/anutron/iris/internal/config"
)

// leaseHarness drives the pure state machine with an injected clock and
// scripted liveness.
type leaseHarness struct {
	t    *testing.T
	st   *LeaseState
	now  time.Time
	cfg  config.DogfoodLeaseTimers
	live map[string]Liveness
	msgs []LeaseMsg
	cans []LeaseCancel
}

func newLeaseHarness(t *testing.T) *leaseHarness {
	return &leaseHarness{
		t:    t,
		st:   &LeaseState{},
		now:  time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		cfg:  config.DefaultDogfoodLeaseTimers(),
		live: map[string]Liveness{},
	}
}

func (h *leaseHarness) m() *leaseMachine {
	return newLeaseMachine(h.st, LeaseEnv{
		Now: h.now, Cfg: h.cfg, RepoName: "iris", DogfoodBranch: "dev", RepoSlug: "iris-abc",
		Live: func(id string) Liveness {
			if lv, ok := h.live[id]; ok {
				if lv.TaskName == "" {
					lv.TaskName = "name-" + id
				}
				return lv
			}
			return Liveness{Class: LiveSilent, TaskName: "name-" + id}
		},
	})
}

// run executes fn against a fresh machine and collects its effects.
func (h *leaseHarness) run(fn func(m *leaseMachine)) {
	m := h.m()
	fn(m)
	h.msgs = append(h.msgs, m.fx.Msgs...)
	h.cans = append(h.cans, m.fx.Cancels...)
}

func (h *leaseHarness) claim(id string, pos int) ClaimOutcome {
	var out ClaimOutcome
	h.run(func(m *leaseMachine) { out = m.Claim(id, "name-"+id, pos) })
	return out
}
func (h *leaseHarness) tick() { h.run(func(m *leaseMachine) { m.Tick() }) }
func (h *leaseHarness) advance(d time.Duration) {
	h.now = h.now.Add(d)
	h.tick()
}
func (h *leaseHarness) take() []LeaseMsg { out := h.msgs; h.msgs = nil; return out }

func (h *leaseHarness) holderID() string {
	if h.st.Holder == nil {
		return ""
	}
	return h.st.Holder.TaskID
}
func (h *leaseHarness) queueIDs() []string {
	ids := []string{}
	for _, q := range h.st.Queue {
		ids = append(ids, q.TaskID)
	}
	return ids
}
func kindsOf(ms []LeaseMsg) []string {
	ks := []string{}
	for _, m := range ms {
		ks = append(ks, m.Kind+":"+m.TaskID)
	}
	return ks
}
func eqStrs(a, b []string) bool { return strings.Join(a, ",") == strings.Join(b, ",") }
func (h *leaseHarness) wantKinds(want ...string) {
	h.t.Helper()
	got := kindsOf(h.take())
	if !eqStrs(got, want) {
		h.t.Fatalf("messages = %v, want %v", got, want)
	}
}
func (h *leaseHarness) wantQueue(want ...string) {
	h.t.Helper()
	if got := h.queueIDs(); !eqStrs(got, want) {
		h.t.Fatalf("queue = %v, want %v", got, want)
	}
}

// ---- grant / queue / poll / re-claim -------------------------------------

func TestLease_FreeLeaseGrantedImmediately(t *testing.T) {
	h := newLeaseHarness(t)
	out := h.claim("A", 0)
	if !out.Granted || out.Position != 0 || h.holderID() != "A" {
		t.Fatalf("out=%+v holder=%s", out, h.holderID())
	}
	msgs := h.take()
	if len(msgs) != 1 || msgs[0].Kind != "grant" || msgs[0].TaskID != "A" {
		t.Fatalf("msgs = %+v", msgs)
	}
	for _, want := range []string{"[iris]", "iris", "dev", "10m", "iris_dogfood_release", "iris_dogfood_renew"} {
		if !strings.Contains(msgs[0].Text, want) {
			t.Errorf("grant text missing %q: %s", want, msgs[0].Text)
		}
	}
	if msgs[0].Deadline != time.Hour {
		t.Errorf("grant deadline = %v, want 1h", msgs[0].Deadline)
	}
	if !h.st.Holder.LastContactAt.Equal(h.now) {
		t.Error("grant counts as contact")
	}
}

func TestLease_BusyLeaseQueuesAndChecksIn(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.take()
	out := h.claim("B", 0)
	if out.Granted || out.Position != 1 || len(out.Ahead) != 0 || out.Holder.TaskID != "A" || out.Holder.TaskName != "name-A" {
		t.Fatalf("out = %+v", out)
	}
	msgs := h.take()
	if len(msgs) != 1 || msgs[0].Kind != "checkin" || msgs[0].TaskID != "A" {
		t.Fatalf("msgs = %+v", msgs)
	}
	if !strings.Contains(msgs[0].Text, "1 agent(s) waiting") {
		t.Errorf("checkin should say one waiting: %s", msgs[0].Text)
	}
	if msgs[0].Deadline != 600*time.Second {
		t.Errorf("checkin deadline = %v, want warmer", msgs[0].Deadline)
	}
	if h.st.Holder.CheckinSentAt.IsZero() {
		t.Error("checkin should be outstanding")
	}
}

func TestLease_QueuedCallerSeesEveryoneAhead(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.claim("C", 0)
	out := h.claim("D", 0)
	if out.Position != 3 || len(out.Ahead) != 2 || out.Ahead[0].TaskID != "B" || out.Ahead[0].Position != 1 || out.Ahead[1].TaskID != "C" || out.Ahead[1].TaskName != "name-C" {
		t.Fatalf("out = %+v", out)
	}
}

func TestLease_CutInLine(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.claim("C", 0)
	out := h.claim("D", 1)
	if out.Position != 1 || len(out.Ahead) != 0 {
		t.Fatalf("out = %+v", out)
	}
	h.wantQueue("D", "B", "C")
	if h.holderID() != "A" {
		t.Fatal("holder must be untouched")
	}
	found := false
	for _, e := range h.st.History {
		if e.Event == "cut_in" && e.TaskID == "D" && strings.Contains(e.Reason, "1") {
			found = true
		}
	}
	if !found {
		t.Fatalf("history missing cut_in: %+v", h.st.History)
	}
}

func TestLease_CutInPositionClamped(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	out := h.claim("C", 9)
	if out.Position != 2 {
		t.Fatalf("position = %d, want 2", out.Position)
	}
	h.wantQueue("B", "C")
}

func TestLease_QueuedCallerMovesSelfWithPosition(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.claim("C", 0)
	out := h.claim("C", 1)
	if out.Position != 1 {
		t.Fatalf("position = %d", out.Position)
	}
	h.wantQueue("C", "B")
}

func TestLease_ReclaimWhileQueuedIsPollNoNewCheckin(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.take()
	out := h.claim("B", 0)
	if out.Granted || out.Position != 1 {
		t.Fatalf("out = %+v", out)
	}
	h.wantKinds() // no second check-in
}

func TestLease_ReclaimByHolderIsContact(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.now = h.now.Add(5 * time.Minute)
	out := h.claim("A", 0)
	if !out.Granted || !h.st.Holder.LastContactAt.Equal(h.now) {
		t.Fatalf("out=%+v contact=%v", out, h.st.Holder.LastContactAt)
	}
}

func TestLease_ClaimAgainstDeadHolderGrantsOnTheSpot(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.take()
	h.live["A"] = Liveness{Class: LiveDead}
	out := h.claim("B", 0)
	if !out.Granted || h.holderID() != "B" {
		t.Fatalf("out=%+v holder=%s", out, h.holderID())
	}
	h.wantKinds("released:A", "grant:B")
}

// ---- queue move ----------------------------------------------------------

func TestLease_QueueMove(t *testing.T) {
	h := newLeaseHarness(t)
	for _, id := range []string{"A", "B", "C", "D"} {
		h.claim(id, 0)
	}
	h.take()
	var q []LeaseRef
	var err error
	h.run(func(m *leaseMachine) { q, err = m.QueueMove("D", 1) })
	if err != nil {
		t.Fatal(err)
	}
	h.wantQueue("D", "B", "C")
	if len(q) != 3 || q[0].TaskID != "D" || q[0].Position != 1 {
		t.Fatalf("returned queue = %+v", q)
	}
	h.wantKinds() // no messages on reorder
	if h.holderID() != "A" {
		t.Fatal("holder untouched")
	}
	// clamp
	h.run(func(m *leaseMachine) { _, err = m.QueueMove("D", 99) })
	h.wantQueue("B", "C", "D")
}

func TestLease_QueueMoveRefusesHolderAndUnqueued(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	before := strings.Join(h.queueIDs(), ",")
	var err error
	h.run(func(m *leaseMachine) { _, err = m.QueueMove("A", 1) })
	if err == nil || !strings.Contains(err.Error(), "holds the lease") {
		t.Fatalf("err = %v", err)
	}
	h.run(func(m *leaseMachine) { _, err = m.QueueMove("Z", 1) })
	if err == nil || !strings.Contains(err.Error(), "not queued") {
		t.Fatalf("err = %v", err)
	}
	if strings.Join(h.queueIDs(), ",") != before {
		t.Fatal("state changed")
	}
}

// ---- refusal view --------------------------------------------------------

func TestLease_RefusalForUnqueuedCallerListsWholeQueue(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.claim("C", 0)
	r := h.m().Refusal("D")
	if r.Holder == nil || r.Holder.TaskID != "A" || len(r.Ahead) != 2 || r.Position != 0 {
		t.Fatalf("refusal = %+v", r)
	}
	r = h.m().Refusal("C")
	if r.Position != 2 || len(r.Ahead) != 1 || r.Ahead[0].TaskID != "B" {
		t.Fatalf("refusal for queued = %+v", r)
	}
}

// ---- renew / release ------------------------------------------------------

func TestLease_RenewRestartsTimerAndClearsCheckin(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0) // immediate check-in
	h.take()
	h.now = h.now.Add(10 * time.Second)
	var ref *LeaseRefusal
	h.run(func(m *leaseMachine) { ref = m.Renew("A") })
	if ref != nil {
		t.Fatalf("renew refused: %+v", ref)
	}
	if !h.st.Holder.CheckinSentAt.IsZero() || !h.st.Holder.LastContactAt.Equal(h.now) {
		t.Fatalf("holder = %+v", h.st.Holder)
	}
	if len(h.cans) != 1 || h.cans[0].TaskID != "A" {
		t.Fatalf("cancels = %+v", h.cans)
	}
}

func TestLease_RenewByNonHolderRefused(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	var ref *LeaseRefusal
	h.run(func(m *leaseMachine) { ref = m.Renew("B") })
	if ref == nil || ref.Holder.TaskID != "A" || ref.Position != 1 {
		t.Fatalf("ref = %+v", ref)
	}
}

func TestLease_ReleaseHolderGrantsNext(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.take()
	var out ReleaseOutcome
	h.run(func(m *leaseMachine) { out = m.Release("A") })
	if !out.Released || out.Was != "holder" || out.GrantedTo != "B" || h.holderID() != "B" {
		t.Fatalf("out = %+v", out)
	}
	h.wantKinds("grant:B") // voluntary: no release notice to A
}

func TestLease_ReleaseQueuedAndNone(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.claim("C", 0)
	var out ReleaseOutcome
	h.run(func(m *leaseMachine) { out = m.Release("B") })
	if !out.Released || out.Was != "queued" {
		t.Fatalf("out = %+v", out)
	}
	h.wantQueue("C")
	h.run(func(m *leaseMachine) { out = m.Release("Z") })
	if out.Released || out.Was != "none" {
		t.Fatalf("out = %+v", out)
	}
}

func TestLease_DeadQueuedEntrySkippedAtGrant(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.claim("C", 0)
	h.live["B"] = Liveness{Class: LiveDead}
	h.take()
	h.run(func(m *leaseMachine) { m.Release("A") })
	if h.holderID() != "C" {
		t.Fatalf("holder = %s", h.holderID())
	}
	h.wantQueue()
	h.wantKinds("grant:C")
}

func TestLease_ReleaseWithNoLiveWaitersFreesLease(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.live["B"] = Liveness{Class: LiveDead}
	h.run(func(m *leaseMachine) { m.Release("A") })
	if h.st.Holder != nil || len(h.st.Queue) != 0 {
		t.Fatalf("lease should be free: %+v", h.st)
	}
}

func TestLease_ForceRelease(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.live["A"] = Liveness{Class: LiveBusy}
	h.st.Holder.InFlight = 1 // force ignores in-flight
	h.take()
	var ok bool
	h.run(func(m *leaseMachine) { ok, _ = m.ForceRelease("A is looping") })
	if !ok || h.holderID() != "B" {
		t.Fatalf("ok=%v holder=%s", ok, h.holderID())
	}
	msgs := h.take()
	if len(msgs) != 2 || msgs[0].Kind != "released" || !strings.Contains(msgs[0].Text, "A is looping") || msgs[1].Kind != "grant" {
		t.Fatalf("msgs = %+v", msgs)
	}
	last := h.st.History[len(h.st.History)-2]
	if !strings.Contains(last.Reason, "A is looping") {
		t.Fatalf("history = %+v", h.st.History)
	}
	h.run(func(m *leaseMachine) { m.ForceRelease("again") })
	h.run(func(m *leaseMachine) { ok, _ = m.ForceRelease("x") })
	if h.st.Holder != nil {
		t.Fatal("second force-release frees")
	}
	h.run(func(m *leaseMachine) { ok, _ = m.ForceRelease("x") })
	if ok {
		t.Fatal("no holder → released=false")
	}
}

// ---- timers --------------------------------------------------------------

func TestLease_WarmerCheckinAfterInterval(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.take()
	h.advance(599 * time.Second)
	h.wantKinds()
	h.advance(1 * time.Second)
	msgs := h.take()
	if len(msgs) != 1 || msgs[0].Kind != "checkin" {
		t.Fatalf("msgs = %+v", msgs)
	}
	if strings.Contains(msgs[0].Text, "waiting") {
		t.Errorf("nobody waiting, text should not say so: %s", msgs[0].Text)
	}
	for _, w := range []string{"iris_dogfood_renew", "iris_dogfood_release"} {
		if !strings.Contains(msgs[0].Text, w) {
			t.Errorf("checkin missing %s", w)
		}
	}
}

func TestLease_ContactRestartsWarmer(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.take()
	h.now = h.now.Add(570 * time.Second)
	h.run(func(m *leaseMachine) { m.BeginFlight("A") })
	h.run(func(m *leaseMachine) { m.EndFlight("A") })
	h.advance(60 * time.Second) // original deadline passed
	h.wantKinds()
	h.advance(540 * time.Second)
	h.wantKinds("checkin:A")
}

// Aaron's script: check-in → silence → final warning → silence → release.
func TestLease_SilenceWithWaiterHandsOver(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.wantKinds("grant:A", "checkin:A")
	h.advance(29 * time.Second)
	h.wantKinds()
	h.advance(1 * time.Second) // G elapsed since check-in
	h.wantKinds("final:A")
	if h.st.Holder.ReleaseAt.IsZero() {
		t.Fatal("release should be scheduled")
	}
	h.advance(29 * time.Second)
	h.wantKinds()
	h.advance(1 * time.Second)
	h.wantKinds("released:A", "grant:B")
	if h.holderID() != "B" {
		t.Fatalf("holder = %s", h.holderID())
	}
}

func TestLease_SilenceWithoutWaitersDoesNotRelease(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.take()
	h.advance(600 * time.Second)
	h.wantKinds("checkin:A")
	h.advance(120 * time.Second) // far beyond G
	h.wantKinds()
	if h.holderID() != "A" || !h.st.Holder.ReleaseAt.IsZero() {
		t.Fatal("must keep lease, no scheduled release")
	}
	h.advance(480 * time.Second) // W after the missed check-in
	h.wantKinds("checkin:A")
}

func TestLease_WaiterJoinsWhileCheckinOverdue(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.advance(600 * time.Second) // check-in sent
	h.take()
	h.now = h.now.Add(5 * time.Minute) // ignored for 5 minutes
	h.claim("B", 0)
	h.wantKinds("final:A") // straight to the final warning, no new check-in
}

func TestLease_BusyHolderNotReleasedOnSilence(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.take()
	h.live["A"] = Liveness{Class: LiveBusy}
	h.advance(60 * time.Second)
	h.wantKinds()
	if h.holderID() != "A" || !h.st.Holder.ReleaseAt.IsZero() {
		t.Fatal("busy must defer")
	}
}

func TestLease_NeedsInputCountsAsSilent(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.take()
	h.live["A"] = Liveness{Class: LiveSilent}
	h.advance(30 * time.Second)
	h.wantKinds("final:A")
	h.advance(30 * time.Second)
	h.wantKinds("released:A", "grant:B")
}

func TestLease_NoBridgeUsesPlainSilenceRules(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.take()
	h.live["A"] = Liveness{Class: LiveNoBridge}
	h.advance(30 * time.Second)
	h.advance(30 * time.Second)
	h.wantKinds("final:A", "released:A", "grant:B")
}

func TestLease_UnreachableNeverReleasesOnSilence(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.take()
	h.live["A"] = Liveness{Class: LiveUnreachable, Err: "dial refused"}
	h.advance(30 * time.Second)
	h.advance(300 * time.Second)
	h.wantKinds()
	if h.holderID() != "A" {
		t.Fatal("must not release")
	}
}

func TestLease_FinalWarningThenBusyDefersRelease(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.advance(30 * time.Second) // final warning
	h.take()
	h.live["A"] = Liveness{Class: LiveBusy}
	h.advance(30 * time.Second)
	h.wantKinds()
	if h.holderID() != "A" {
		t.Fatal("busy at release_at must defer")
	}
}

func TestLease_DeadHolderReleasedAtOnceOnTick(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.take()
	h.live["A"] = Liveness{Class: LiveDead}
	h.advance(30 * time.Second) // G after check-in
	h.wantKinds("released:A", "grant:B")
}

func TestLease_DeadHolderAtWarmerCheckinReleasedNoCheckin(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.claim("C", 0)
	h.take()
	h.live["A"] = Liveness{Class: LiveDead}
	// Renew-less: contact clears the immediate check-in so the warmer decides.
	h.run(func(m *leaseMachine) { m.st.Holder.CheckinSentAt = time.Time{}; m.st.Holder.CheckinWanted = false })
	h.advance(600 * time.Second)
	h.wantKinds("released:A", "grant:B")
}

func TestLease_IdleReleaseNobodyWaiting(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.take()
	h.advance(1799 * time.Second)
	h.take()
	if h.holderID() != "A" {
		t.Fatal("released too early")
	}
	h.advance(1 * time.Second)
	h.wantKinds("released:A")
	if h.st.Holder != nil {
		t.Fatal("lease should be free")
	}
}

func TestLease_IdleCapAppliesToBusyHolderWithWaiter(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.live["A"] = Liveness{Class: LiveBusy}
	h.take()
	h.advance(1800 * time.Second)
	h.wantKinds("released:A", "grant:B")
}

func TestLease_InFlightDefersReleases(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.take()
	h.run(func(m *leaseMachine) { m.BeginFlight("A") })
	h.advance(1 * time.Hour)
	h.wantKinds()
	if h.holderID() != "A" {
		t.Fatal("in-flight holder released")
	}
	h.run(func(m *leaseMachine) { m.EndFlight("A") })
	if !h.st.Holder.LastContactAt.Equal(h.now) || h.st.Holder.InFlight != 0 {
		t.Fatalf("finish counts as contact: %+v", h.st.Holder)
	}
	h.advance(10 * time.Second)
	h.wantKinds()
}

func TestLease_BeginFlightOnlyForHolder(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	var ok bool
	h.run(func(m *leaseMachine) { ok = m.BeginFlight("B") })
	if ok || h.st.Holder.InFlight != 0 {
		t.Fatal("non-holder must not mark in-flight")
	}
}

func TestLease_UndeliveredCheckinDoesNotStartSilenceCountdown(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.take()
	h.claim("B", 0)
	msgs := h.take()
	if len(msgs) != 1 || msgs[0].Kind != "checkin" {
		t.Fatalf("msgs = %+v", msgs)
	}
	h.run(func(m *leaseMachine) { m.MarkUndelivered(msgs[0]) })
	if !h.st.Holder.CheckinSentAt.IsZero() {
		t.Fatal("undelivered check-in must not be outstanding")
	}
	h.advance(60 * time.Second) // would be silence if it counted
	got := h.take()
	if len(got) != 1 || got[0].Kind != "checkin" { // retried, not a final warning
		t.Fatalf("expected a retried checkin, got %+v", got)
	}
	if h.holderID() != "A" {
		t.Fatal("must not release")
	}
}

func TestLease_UndeliveredFinalWarningRetried(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.advance(30 * time.Second)
	msgs := h.take()
	var fin LeaseMsg
	for _, m := range msgs {
		if m.Kind == "final" {
			fin = m
		}
	}
	if fin.Kind == "" {
		t.Fatalf("no final warning: %+v", msgs)
	}
	h.run(func(m *leaseMachine) { m.MarkUndelivered(fin) })
	if !h.st.Holder.ReleaseAt.IsZero() {
		t.Fatal("release must not be scheduled on an undelivered warning")
	}
	h.advance(5 * time.Second)
	h.wantKinds("final:A")
}

func TestLease_ContactCancelsPendingDeliveries(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0) // checkin pending
	h.cans = nil
	h.run(func(m *leaseMachine) { m.BeginFlight("A") })
	if len(h.cans) != 1 || h.cans[0].TaskID != "A" || h.cans[0].DeliveryID == "" {
		t.Fatalf("cancels = %+v", h.cans)
	}
	if len(h.st.Holder.PendingDeliveries) != 0 {
		t.Fatal("pending deliveries should be cleared")
	}
}

func TestLease_DeliveryIDsAreValidAndUnique(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.advance(30 * time.Second)
	seen := map[string]bool{}
	for _, m := range h.msgs {
		if len(m.DeliveryID) == 0 || len(m.DeliveryID) > 128 || seen[m.DeliveryID] {
			t.Fatalf("bad/duplicate delivery id %q", m.DeliveryID)
		}
		seen[m.DeliveryID] = true
		for _, r := range m.DeliveryID {
			if !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
				t.Fatalf("bad char %q in %q", r, m.DeliveryID)
			}
		}
		if !strings.HasPrefix(m.DeliveryID, "iris-lease-iris-abc-"+m.Kind+"-") {
			t.Fatalf("delivery id format: %q", m.DeliveryID)
		}
	}
}

func TestLease_ConfiguredTimersApply(t *testing.T) {
	h := newLeaseHarness(t)
	h.cfg = config.DogfoodLeaseTimers{WarmerSeconds: 300, GraceSeconds: 10, IdleReleaseSeconds: 900}
	h.claim("A", 0)
	if msgs := h.take(); !strings.Contains(msgs[0].Text, "5m") {
		t.Errorf("grant should state the 5m timer: %s", msgs[0].Text)
	}
	h.advance(299 * time.Second)
	h.wantKinds()
	h.advance(1 * time.Second)
	h.wantKinds("checkin:A")
}

// ---- restart -------------------------------------------------------------

func TestLease_RecoverAfterRestartPushesOutDeadlinesKeepsContact(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0)
	h.advance(30 * time.Second) // final warning, release_at = +30s
	h.take()
	contact := h.st.Holder.LastContactAt
	release := h.st.Holder.ReleaseAt
	// iris is down for 2 minutes.
	startup := h.now.Add(2 * time.Minute)
	RecoverLeaseAfterRestart(h.st, startup, h.cfg)
	if !h.st.Holder.LastContactAt.Equal(contact) {
		t.Fatal("last_contact_at must be unchanged")
	}
	if want := startup.Add(30 * time.Second); !h.st.Holder.ReleaseAt.Equal(want) || !h.st.Holder.ReleaseAt.After(release) {
		t.Fatalf("release_at = %v, want %v", h.st.Holder.ReleaseAt, want)
	}
	h.now = startup
	h.tick()
	h.wantKinds() // no release at startup
	h.run(func(m *leaseMachine) { m.Renew("A") })
	h.advance(60 * time.Second)
	h.wantKinds()
	if h.holderID() != "A" {
		t.Fatal("contact after restart must cancel the release")
	}
}

func TestLease_RecoverRestartsOutstandingCheckinWindow(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.claim("B", 0) // check-in outstanding since now
	h.take()
	startup := h.now.Add(10 * time.Minute)
	RecoverLeaseAfterRestart(h.st, startup, h.cfg)
	h.now = startup
	h.tick()
	h.wantKinds() // reply window restarted: no final warning yet
	h.advance(30 * time.Second)
	h.wantKinds("final:A")
}

func TestLease_RecoverClearsInFlight(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.st.Holder.InFlight = 2
	RecoverLeaseAfterRestart(h.st, h.now, h.cfg)
	if h.st.Holder.InFlight != 0 {
		t.Fatal("a restart kills in-flight calls")
	}
}

func TestLease_RecoverIdleCapStillHolds(t *testing.T) {
	h := newLeaseHarness(t)
	h.claim("A", 0)
	h.take()
	startup := h.now.Add(2 * time.Hour)
	RecoverLeaseAfterRestart(h.st, startup, h.cfg)
	h.now = startup
	h.tick()
	h.wantKinds("released:A")
}

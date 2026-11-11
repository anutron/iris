package verbs

// Pure dogfood-lease state machine (design D3). Nothing here does I/O: the
// machine mutates a LeaseState in place and records the messages to send and
// deliveries to cancel in Effects, which the caller (dogfood_lease.go) then
// executes. Liveness is supplied by a callback so tests can script it. Time
// comes from LeaseEnv.Now so tests inject the clock.

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/anutron/iris/internal/config"
)

const (
	leaseHistoryMax = 20
	// leaseMaxDeadline is argus's maximum notify deadline.
	leaseMaxDeadline = time.Hour

	leaseMsgGrant     = "grant"
	leaseMsgCheckin   = "checkin"
	leaseMsgFinal     = "final"
	leaseMsgReleased  = "released"
	leaseMsgOverride  = "override"
	leaseMsgRecompose = "recompose_skipped"
)

// LeaseHolder is the current holder.
type LeaseHolder struct {
	TaskID        string    `json:"task_id"`
	TaskName      string    `json:"task_name"`
	GrantedAt     time.Time `json:"granted_at"`
	LastContactAt time.Time `json:"last_contact_at"`
	// CheckinSentAt is set while a delivered check-in awaits a reply.
	CheckinSentAt  time.Time `json:"checkin_sent_at,omitzero"`
	FinalWarningAt time.Time `json:"final_warning_at,omitzero"`
	ReleaseAt      time.Time `json:"release_at,omitzero"`
	// CheckinWanted requests an immediate check-in (a waiter joined, or a
	// check-in failed to deliver). Cleared when one is sent or on contact.
	CheckinWanted bool `json:"checkin_wanted,omitempty"`
	// InFlight counts the holder's running set_dogfood calls. Never persisted
	// meaningfully: a restart kills the calls, so recovery zeroes it.
	InFlight int `json:"in_flight,omitempty"`
	// PendingDeliveries are check-in/final-warning delivery IDs to cancel on contact.
	PendingDeliveries []string `json:"pending_deliveries,omitempty"`
}

// LeaseEntry is a queued task.
type LeaseEntry struct {
	TaskID     string    `json:"task_id"`
	TaskName   string    `json:"task_name"`
	EnqueuedAt time.Time `json:"enqueued_at"`
}

// LeaseHistoryEntry records one transition.
type LeaseHistoryEntry struct {
	At     time.Time `json:"at"`
	Event  string    `json:"event"`
	TaskID string    `json:"task_id,omitempty"`
	Reason string    `json:"reason,omitempty"`
}

// LeaseState is the persisted state for one source repo.
type LeaseState struct {
	// SourceRepo is stored so the startup scan can find the repo a state file
	// belongs to (the state directory name is a one-way hash).
	SourceRepo string              `json:"source_repo,omitempty"`
	Holder     *LeaseHolder        `json:"holder"`
	Queue      []LeaseEntry        `json:"queue"`
	History    []LeaseHistoryEntry `json:"history"`
}

// LeaseRef names a task for results and refusals.
type LeaseRef struct {
	TaskID   string `json:"task_id"`
	TaskName string `json:"task_name"`
	Position int    `json:"position,omitempty"`
}

// LeaseMsg is a message to deliver through argus pane delivery.
type LeaseMsg struct {
	TaskID     string
	Kind       string
	Text       string
	DeliveryID string
	Deadline   time.Duration
}

// LeaseCancel asks the executor to cancel a pending delivery.
type LeaseCancel struct{ TaskID, DeliveryID string }

// LeaseEffects are the side effects the machine wants performed.
type LeaseEffects struct {
	Msgs    []LeaseMsg
	Cancels []LeaseCancel
}

// LeaseEnv is everything the machine needs besides the state.
type LeaseEnv struct {
	Now           time.Time
	Cfg           config.DogfoodLeaseTimers
	RepoName      string
	RepoSlug      string
	DogfoodBranch string
	Live          func(taskID string) Liveness
}

// LeaseRefusal explains why a caller does not hold the lease.
type LeaseRefusal struct {
	Holder   *LeaseRef  `json:"holder"`
	Ahead    []LeaseRef `json:"ahead"`
	Position int        `json:"position"`
}

func (r *LeaseRefusal) holderText() string {
	if r.Holder == nil {
		return "no one holds the dogfood lease"
	}
	return fmt.Sprintf("the dogfood lease is held by %s (%s)", r.Holder.TaskName, r.Holder.TaskID)
}

// Error renders the refusal for renew.
func (r *LeaseRefusal) Error() string {
	var b strings.Builder
	b.WriteString("you do not hold the dogfood lease: " + r.holderText())
	if r.Position > 0 {
		fmt.Fprintf(&b, "; you are queued at position %d", r.Position)
	}
	if len(r.Ahead) > 0 {
		b.WriteString("; ahead of you: " + refList(r.Ahead))
	}
	return b.String()
}

func refList(refs []LeaseRef) string {
	parts := make([]string, 0, len(refs))
	for _, r := range refs {
		parts = append(parts, fmt.Sprintf("%d. %s (%s)", r.Position, r.TaskName, r.TaskID))
	}
	return strings.Join(parts, ", ")
}

// ClaimOutcome is the result of Claim.
type ClaimOutcome struct {
	Granted       bool
	Position      int
	Holder        LeaseRef
	Ahead         []LeaseRef
	QueueLength   int
	NextCheckinAt time.Time
}

// ReleaseOutcome is the result of Release.
type ReleaseOutcome struct {
	Released  bool
	Was       string // holder | queued | none
	GrantedTo string
}

type leaseMachine struct {
	st  *LeaseState
	env LeaseEnv
	fx  LeaseEffects
}

func newLeaseMachine(st *LeaseState, env LeaseEnv) *leaseMachine {
	return &leaseMachine{st: st, env: env}
}

var leaseDeliverySeq atomic.Int64

func (m *leaseMachine) cfgDur(secs int) time.Duration { return time.Duration(secs) * time.Second }
func (m *leaseMachine) warmer() time.Duration         { return m.cfgDur(m.env.Cfg.WarmerSeconds) }
func (m *leaseMachine) grace() time.Duration          { return m.cfgDur(m.env.Cfg.GraceSeconds) }
func (m *leaseMachine) idle() time.Duration           { return m.cfgDur(m.env.Cfg.IdleReleaseSeconds) }

func fmtLeaseDur(secs int) string {
	if secs%60 == 0 {
		return fmt.Sprintf("%dm", secs/60)
	}
	return fmt.Sprintf("%ds", secs)
}

func slugChars(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func (m *leaseMachine) deliveryID(kind string) string {
	slug := slugChars(m.env.RepoSlug)
	if len(slug) > 60 {
		slug = slug[:60]
	}
	n := m.env.Now.UnixNano()
	for {
		last := leaseDeliverySeq.Load()
		if n <= last {
			n = last + 1
		}
		if leaseDeliverySeq.CompareAndSwap(last, n) {
			break
		}
	}
	return fmt.Sprintf("iris-lease-%s-%s-%d", slug, kind, n)
}

// notify queues a message to a task and returns it.
func (m *leaseMachine) notify(to, kind, text string, deadline time.Duration) LeaseMsg {
	msg := LeaseMsg{TaskID: to, Kind: kind, Text: text, DeliveryID: m.deliveryID(kind), Deadline: deadline}
	m.fx.Msgs = append(m.fx.Msgs, msg)
	return msg
}

// Notice queues a free-form notice (override / recompose-skipped) to a task.
func (m *leaseMachine) Notice(to, kind, text string) {
	m.notify(to, kind, text, leaseMaxDeadline)
}

func (m *leaseMachine) history(event, taskID, reason string) {
	m.st.History = append(m.st.History, LeaseHistoryEntry{At: m.env.Now, Event: event, TaskID: taskID, Reason: reason})
	if n := len(m.st.History); n > leaseHistoryMax {
		m.st.History = append([]LeaseHistoryEntry(nil), m.st.History[n-leaseHistoryMax:]...)
	}
}

func (m *leaseMachine) queueIndex(id string) int {
	for i, q := range m.st.Queue {
		if q.TaskID == id {
			return i
		}
	}
	return -1
}

func (m *leaseMachine) refs(entries []LeaseEntry, firstPos int) []LeaseRef {
	out := make([]LeaseRef, 0, len(entries))
	for i, q := range entries {
		out = append(out, LeaseRef{TaskID: q.TaskID, TaskName: q.TaskName, Position: firstPos + i})
	}
	return out
}

// ---- views ---------------------------------------------------------------

func (m *leaseMachine) holderRef() LeaseRef {
	if m.st.Holder == nil {
		return LeaseRef{}
	}
	return LeaseRef{TaskID: m.st.Holder.TaskID, TaskName: m.st.Holder.TaskName}
}

// Refusal builds the "you don't hold it" view for a caller (renew refusal,
// set_dogfood gate). Ahead is everyone in front of the caller; for a caller
// that is not queued, the whole queue (D1b).
func (m *leaseMachine) Refusal(id string) *LeaseRefusal {
	r := &LeaseRefusal{Ahead: []LeaseRef{}}
	if m.st.Holder != nil {
		h := m.holderRef()
		r.Holder = &h
	}
	if i := m.queueIndex(id); i >= 0 {
		r.Position = i + 1
		r.Ahead = m.refs(m.st.Queue[:i], 1)
	} else {
		r.Ahead = m.refs(m.st.Queue, 1)
	}
	return r
}

func (m *leaseMachine) nextCheckinAt() time.Time {
	if m.st.Holder == nil {
		return time.Time{}
	}
	return m.st.Holder.LastContactAt.Add(m.warmer())
}

func (m *leaseMachine) queued(id, name string, pos int) ClaimOutcome {
	r := m.Refusal(id)
	return ClaimOutcome{Position: pos, Holder: m.holderRef(), Ahead: r.Ahead, QueueLength: len(m.st.Queue)}
}

// ---- operations ----------------------------------------------------------

// Claim implements iris:dogfood_claim's state change. position is 1-based; 0
// means append.
func (m *leaseMachine) Claim(id, name string, position int) ClaimOutcome {
	if m.st.Holder == nil {
		m.grant(id, name)
		return m.granted()
	}
	if m.st.Holder.TaskID == id {
		if name != "" {
			m.st.Holder.TaskName = name
		}
		m.contact()
		return m.granted()
	}
	if i := m.queueIndex(id); i >= 0 {
		if name != "" {
			m.st.Queue[i].TaskName = name
		}
		if position > 0 {
			m.moveTo(i, position)
			m.history("cut_in", id, fmt.Sprintf("moved to position %d", m.queueIndex(id)+1))
		}
		return m.queued(id, name, m.queueIndex(id)+1)
	}
	// New claimant: a dead holder is released first (unless mid-deploy).
	if h := m.st.Holder; h.InFlight == 0 {
		lv := m.env.Live(h.TaskID)
		m.noteName(h.TaskID, lv)
		if lv.Class == LiveDead {
			m.release("dead", false)
			if m.st.Holder == nil {
				m.grant(id, name)
				return m.granted()
			}
		}
	}
	// A release above may have granted someone else; the caller still queues.
	e := LeaseEntry{TaskID: id, TaskName: name, EnqueuedAt: m.env.Now}
	pos := len(m.st.Queue) + 1
	if position > 0 && position < pos {
		pos = position
		m.history("cut_in", id, fmt.Sprintf("cut in at position %d", pos))
	} else {
		m.history("join", id, "")
	}
	q := append([]LeaseEntry(nil), m.st.Queue[:pos-1]...)
	q = append(q, e)
	m.st.Queue = append(q, m.st.Queue[pos-1:]...)
	if m.st.Holder.CheckinSentAt.IsZero() {
		m.st.Holder.CheckinWanted = true
	}
	m.Tick()
	// Tick may have released the holder and granted us.
	if m.st.Holder != nil && m.st.Holder.TaskID == id {
		return m.granted()
	}
	return m.queued(id, name, m.queueIndex(id)+1)
}

func (m *leaseMachine) granted() ClaimOutcome {
	return ClaimOutcome{Granted: true, Holder: m.holderRef(), Ahead: []LeaseRef{}, QueueLength: len(m.st.Queue), NextCheckinAt: m.nextCheckinAt()}
}

// moveTo moves queue[i] to 1-based position pos, clamped.
func (m *leaseMachine) moveTo(i, pos int) {
	e := m.st.Queue[i]
	rest := append([]LeaseEntry(nil), m.st.Queue[:i]...)
	rest = append(rest, m.st.Queue[i+1:]...)
	if pos < 1 {
		pos = 1
	}
	if pos > len(rest)+1 {
		pos = len(rest) + 1
	}
	out := append([]LeaseEntry(nil), rest[:pos-1]...)
	out = append(out, e)
	m.st.Queue = append(out, rest[pos-1:]...)
}

// QueueMove reorders the queue (iris:dogfood_queue_move). No messages.
func (m *leaseMachine) QueueMove(id string, position int) ([]LeaseRef, error) {
	i := m.queueIndex(id)
	if i < 0 {
		if m.st.Holder != nil && m.st.Holder.TaskID == id {
			return nil, fmt.Errorf("task %s holds the lease and is not in the queue; only queued tasks can be moved", id)
		}
		return nil, fmt.Errorf("task %s is not queued", id)
	}
	m.moveTo(i, position)
	m.history("move", id, fmt.Sprintf("moved to position %d", m.queueIndex(id)+1))
	return m.refs(m.st.Queue, 1), nil
}

// Renew is the holder's "still using it" reply. A non-nil result is a refusal.
func (m *leaseMachine) Renew(id string) *LeaseRefusal {
	if m.st.Holder == nil || m.st.Holder.TaskID != id {
		return m.Refusal(id)
	}
	m.contact()
	m.history("renew", id, "")
	return nil
}

// Release implements iris:dogfood_release.
func (m *leaseMachine) Release(id string) ReleaseOutcome {
	if m.st.Holder != nil && m.st.Holder.TaskID == id {
		m.release("released by holder", true)
		out := ReleaseOutcome{Released: true, Was: "holder"}
		if m.st.Holder != nil {
			out.GrantedTo = m.st.Holder.TaskID
		}
		return out
	}
	if i := m.queueIndex(id); i >= 0 {
		m.st.Queue = append(m.st.Queue[:i:i], m.st.Queue[i+1:]...)
		m.history("leave", id, "left the queue")
		return ReleaseOutcome{Released: true, Was: "queued"}
	}
	return ReleaseOutcome{Was: "none"}
}

// ForceRelease evicts the holder whatever its state.
func (m *leaseMachine) ForceRelease(reason string) (bool, string) {
	if m.st.Holder == nil {
		return false, ""
	}
	m.release("force release: "+reason, false)
	if m.st.Holder != nil {
		return true, m.st.Holder.TaskID
	}
	return true, ""
}

// BeginFlight marks a holder's set_dogfood call as running and counts as
// contact. Returns false when id is not the holder.
func (m *leaseMachine) BeginFlight(id string) bool {
	if m.st.Holder == nil || m.st.Holder.TaskID != id {
		return false
	}
	m.contact()
	m.st.Holder.InFlight++
	return true
}

// EndFlight marks the call finished; the finish counts as contact.
func (m *leaseMachine) EndFlight(id string) {
	if m.st.Holder == nil || m.st.Holder.TaskID != id {
		return
	}
	if m.st.Holder.InFlight > 0 {
		m.st.Holder.InFlight--
	}
	m.contact()
}

// Touch records contact from id when it is the holder (e.g. a recompose by
// the holder).
func (m *leaseMachine) Touch(id string) {
	if m.st.Holder != nil && m.st.Holder.TaskID == id {
		m.contact()
	}
}

// MarkUndelivered undoes a message whose delivery failed so the countdown it
// would have started does not run, and the message is retried next tick.
func (m *leaseMachine) MarkUndelivered(msg LeaseMsg) {
	h := m.st.Holder
	if h == nil || h.TaskID != msg.TaskID {
		return
	}
	// Only a still-pending delivery is undone: contact may have cancelled it.
	if !containsStr(h.PendingDeliveries, msg.DeliveryID) {
		return
	}
	h.PendingDeliveries = removeStr(h.PendingDeliveries, msg.DeliveryID)
	switch msg.Kind {
	case leaseMsgCheckin:
		h.CheckinSentAt = time.Time{}
		h.CheckinWanted = true
	case leaseMsgFinal:
		h.FinalWarningAt = time.Time{}
		h.ReleaseAt = time.Time{}
	}
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func removeStr(ss []string, s string) []string {
	out := ss[:0:0]
	for _, x := range ss {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

// ---- internals -----------------------------------------------------------

func (m *leaseMachine) noteName(id string, lv Liveness) {
	if lv.TaskName == "" {
		return
	}
	if h := m.st.Holder; h != nil && h.TaskID == id {
		h.TaskName = lv.TaskName
	}
	for i := range m.st.Queue {
		if m.st.Queue[i].TaskID == id {
			m.st.Queue[i].TaskName = lv.TaskName
		}
	}
}

// contact resets the holder's clocks and cancels undelivered check-ins.
func (m *leaseMachine) contact() {
	h := m.st.Holder
	if h == nil {
		return
	}
	h.LastContactAt = m.env.Now
	h.CheckinSentAt, h.FinalWarningAt, h.ReleaseAt = time.Time{}, time.Time{}, time.Time{}
	h.CheckinWanted = false
	for _, d := range h.PendingDeliveries {
		m.fx.Cancels = append(m.fx.Cancels, LeaseCancel{TaskID: h.TaskID, DeliveryID: d})
	}
	h.PendingDeliveries = nil
}

func (m *leaseMachine) grant(id, name string) {
	m.st.Holder = &LeaseHolder{TaskID: id, TaskName: name, GrantedAt: m.env.Now, LastContactAt: m.env.Now}
	m.history("grant", id, "")
	m.notify(id, leaseMsgGrant, fmt.Sprintf(
		"[iris] You have the dogfood lease for %s (branch %s). %s timer is running — any iris_set_dogfood / iris_dogfood_renew call resets it. Call iris_dogfood_release when you're done.",
		m.env.RepoName, m.env.DogfoodBranch, fmtLeaseDur(m.env.Cfg.WarmerSeconds)), leaseMaxDeadline)
}

// release ends the current hold and grants the next live queued task.
func (m *leaseMachine) release(reason string, voluntary bool) {
	old := m.st.Holder
	if old == nil {
		return
	}
	for _, d := range old.PendingDeliveries {
		m.fx.Cancels = append(m.fx.Cancels, LeaseCancel{TaskID: old.TaskID, DeliveryID: d})
	}
	m.st.Holder = nil
	m.history("release", old.TaskID, reason)
	if !voluntary {
		m.notify(old.TaskID, leaseMsgReleased, fmt.Sprintf(
			"[iris] Released your dogfood lease for %s (%s). Call iris_dogfood_claim if you need it again.",
			m.env.RepoName, reason), leaseMaxDeadline)
	}
	m.grantNext()
}

// grantNext pops queue entries until one is not dead and grants it.
func (m *leaseMachine) grantNext() {
	for len(m.st.Queue) > 0 {
		e := m.st.Queue[0]
		m.st.Queue = m.st.Queue[1:]
		lv := m.env.Live(e.TaskID)
		if lv.Class == LiveDead {
			m.history("drop", e.TaskID, "task is dead")
			continue
		}
		name := e.TaskName
		if lv.TaskName != "" {
			name = lv.TaskName
		}
		m.grant(e.TaskID, name)
		return
	}
}

// Tick evaluates the timers (D3 steps 2-5, 8). Callers hold the lease mutex.
func (m *leaseMachine) Tick() {
	h := m.st.Holder
	if h == nil {
		if len(m.st.Queue) > 0 { // invariant repair: waiters never sit on a free lease
			m.grantNext()
		}
		return
	}
	if h.InFlight > 0 {
		return
	}
	now := m.env.Now
	waiting := len(m.st.Queue)

	// Idle-release hard cap: independent of waiters and liveness.
	if !now.Before(h.LastContactAt.Add(m.idle())) {
		m.release(fmt.Sprintf("no word for %s", fmtLeaseDur(m.env.Cfg.IdleReleaseSeconds)), false)
		return
	}

	// Scheduled release after the final warning.
	if !h.ReleaseAt.IsZero() {
		if now.Before(h.ReleaseAt) {
			return
		}
		lv := m.env.Live(h.TaskID)
		m.noteName(h.TaskID, lv)
		switch lv.Class {
		case LiveDead:
			m.release("task is no longer running", false)
		case LiveSilent, LiveNoBridge:
			m.release("no answer to the check-in", false)
		}
		return // busy / unreachable: defer
	}

	// A delivered check-in awaits a reply.
	if !h.CheckinSentAt.IsZero() {
		switch {
		case waiting > 0 && !now.Before(h.CheckinSentAt.Add(m.grace())):
			lv := m.env.Live(h.TaskID)
			m.noteName(h.TaskID, lv)
			switch lv.Class {
			case LiveDead:
				m.release("task is no longer running", false)
			case LiveSilent, LiveNoBridge:
				h.FinalWarningAt = now
				h.ReleaseAt = now.Add(m.grace())
				msg := m.notify(h.TaskID, leaseMsgFinal, fmt.Sprintf(
					"[iris] No answer — releasing the dogfood lease for %s in %ds unless you call iris_dogfood_renew.",
					m.env.RepoName, m.env.Cfg.GraceSeconds), m.warmer())
				h.PendingDeliveries = append(h.PendingDeliveries, msg.DeliveryID)
			}
			return
		case waiting == 0 && !now.Before(h.CheckinSentAt.Add(m.warmer())):
			h.CheckinSentAt = time.Time{} // missed check-in: ask again, W after it
		default:
			return
		}
	}

	// A check-in is due: the warmer elapsed, or one was requested.
	if h.CheckinWanted || !now.Before(h.LastContactAt.Add(m.warmer())) {
		lv := m.env.Live(h.TaskID)
		m.noteName(h.TaskID, lv)
		if lv.Class == LiveDead {
			m.release("task is no longer running", false)
			return
		}
		text := fmt.Sprintf("[iris] Still using dogfood for %s? Reply by calling iris_dogfood_renew (yes) or iris_dogfood_release (done).", m.env.RepoName)
		if waiting > 0 {
			text += fmt.Sprintf(" %d agent(s) waiting.", waiting)
		}
		msg := m.notify(h.TaskID, leaseMsgCheckin, text, m.warmer())
		h.CheckinSentAt = now
		h.CheckinWanted = false
		h.PendingDeliveries = append(h.PendingDeliveries, msg.DeliveryID)
	}
}

// RecoverLeaseAfterRestart applies D4: an iris restart never counts as holder
// silence. Release and reply deadlines restart from startup; last_contact_at
// is untouched so the idle cap still holds; in-flight calls died with iris.
func RecoverLeaseAfterRestart(st *LeaseState, startup time.Time, cfg config.DogfoodLeaseTimers) {
	h := st.Holder
	if h == nil {
		return
	}
	h.InFlight = 0
	grace := time.Duration(cfg.GraceSeconds) * time.Second
	if !h.ReleaseAt.IsZero() {
		if floor := startup.Add(grace); h.ReleaseAt.Before(floor) {
			h.ReleaseAt = floor
		}
	}
	if !h.CheckinSentAt.IsZero() && h.CheckinSentAt.Before(startup) {
		h.CheckinSentAt = startup
	}
}

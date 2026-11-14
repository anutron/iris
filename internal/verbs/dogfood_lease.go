package verbs

// iris:dogfood_* — the dogfood lease and ordered queue (change
// dogfood-lease-queue). This file is the I/O shell around the pure state
// machine in dogfood_lease_state.go: persistence, the per-repo lease mutex,
// argus liveness lookups, pane delivery, the verbs, the set_dogfood gate and
// the daemon scheduler.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
)

// LeaseFilename is the persisted lease file inside SourceRepoStateDir.
const LeaseFilename = "dogfood-lease.json"

const (
	leaseMaxWaitSeconds = 25
	leaseArgusTimeout   = 5 * time.Second
)

// leasePollInterval is how often a waiting claim re-reads the lease. A var so
// tests can shrink it.
var leasePollInterval = 250 * time.Millisecond

// ---- persistence ---------------------------------------------------------

func leasePath(sourceRepo string) (string, error) {
	dir, err := SourceRepoStateDir(sourceRepo)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, LeaseFilename), nil
}

// loadLease reads the lease file. A missing file is an empty (free) lease.
// A malformed file is an error.
func loadLease(sourceRepo string) (*LeaseState, error) {
	p, err := leasePath(sourceRepo)
	if err != nil {
		return nil, err
	}
	return readLeaseFile(p)
}

func readLeaseFile(p string) (*LeaseState, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &LeaseState{}, nil
		}
		return nil, err
	}
	var st LeaseState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return &st, nil
}

func marshalLease(st *LeaseState) ([]byte, error) {
	if st.Queue == nil {
		st.Queue = []LeaseEntry{}
	}
	if st.History == nil {
		st.History = []LeaseHistoryEntry{}
	}
	return json.MarshalIndent(st, "", "  ")
}

// saveLease writes the lease atomically: temp file, fsync, rename.
func saveLease(sourceRepo string, st *LeaseState) error {
	p, err := leasePath(sourceRepo)
	if err != nil {
		return err
	}
	st.SourceRepo = sourceRepo
	body, err := marshalLease(st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), LeaseFilename+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, p); err != nil {
		cleanup()
		return err
	}
	return nil
}

// ---- per-repo lease mutex (separate from the git mutex) -------------------

var leaseLocks sync.Map

// lockLease takes the repo's lease lock and returns its unlock func. It is an
// in-process mutex plus an flock on a sibling lock file, because CLI commands
// (iris dogfood ...) run in a different process from the daemon scheduler and
// both read-modify-write dogfood-lease.json.
func lockLease(sourceRepo string) func() {
	m, _ := leaseLocks.LoadOrStore(sourceRepo, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	var lockFile *os.File
	if p, err := leasePath(sourceRepo); err == nil {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
			if f, err := os.OpenFile(p+".lock", os.O_CREATE|os.O_RDWR, 0o644); err == nil {
				if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err == nil {
					lockFile = f
				} else {
					f.Close()
				}
			}
		}
	}
	return func() {
		if lockFile != nil {
			_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
			lockFile.Close()
		}
		mu.Unlock()
	}
}

// ---- per-repo in-memory diagnostics (liveness / delivery errors) ----------

type leaseDiag struct{ liveness, delivery string }

var (
	leaseDiagMu sync.Mutex
	leaseDiags  = map[string]*leaseDiag{}
)

func setLeaseDiag(sourceRepo string, fn func(d *leaseDiag)) {
	leaseDiagMu.Lock()
	defer leaseDiagMu.Unlock()
	d := leaseDiags[sourceRepo]
	if d == nil {
		d = &leaseDiag{}
		leaseDiags[sourceRepo] = d
	}
	fn(d)
}

func getLeaseDiag(sourceRepo string) leaseDiag {
	leaseDiagMu.Lock()
	defer leaseDiagMu.Unlock()
	if d := leaseDiags[sourceRepo]; d != nil {
		return *d
	}
	return leaseDiag{}
}

// ---- repo config ---------------------------------------------------------

// leaseRepo is a source repo's lease-relevant configuration.
type leaseRepo struct {
	SourceRepo string
	Name       string
	Slug       string
	Branch     string
	Timers     config.DogfoodLeaseTimers
	Warnings   []string // invalid timer config → defaults used
}

// errDogfoodBranchUnset is the shared refusal for a repo without dogfood_branch.
const errDogfoodBranchUnset = `dogfood_branch not configured for this repo (add dogfood_branch = "..." to .iris.local.toml)`

// resolveDogfoodConfig reads the merged overlay (bootstrap-aware) for the
// repo's dogfood_branch and [dogfood_lease] block. The returned overlay may be
// nil-docced when .iris.toml is absent.
func resolveDogfoodConfig(ctx context.Context, sourceRepo string) (branch string, block *config.DogfoodLeaseBlock, overlay *config.OverlayResult, err error) {
	isSelf, _ := isSelfTarget(ctx, sourceRepo)
	overlay, err = config.LoadOverlay(sourceRepo, isSelf)
	if err != nil {
		return "", nil, nil, err
	}
	if overlay.Doc != nil {
		branch = overlay.Doc.DogfoodBranch
		block = overlay.Doc.DogfoodLease
	}
	if branch == "" {
		branch = config.PeekLocalDogfoodBranch(sourceRepo)
	}
	if overlay.Doc == nil {
		block = config.PeekLocalDogfoodLease(sourceRepo)
	}
	return branch, block, overlay, nil
}

func resolveLeaseRepo(ctx context.Context, sourceRepo string) (*leaseRepo, error) {
	branch, block, _, err := resolveDogfoodConfig(ctx, sourceRepo)
	if err != nil {
		return nil, err
	}
	if branch == "" {
		return nil, errors.New(errDogfoodBranchUnset)
	}
	timers, verrs := config.ValidDogfoodLeaseTimers(block)
	lr := &leaseRepo{
		SourceRepo: sourceRepo,
		Name:       filepath.Base(sourceRepo),
		Slug:       repoStateSlug(sourceRepo),
		Branch:     branch,
		Timers:     timers,
	}
	for _, e := range verrs {
		lr.Warnings = append(lr.Warnings, "invalid [dogfood_lease] config, using defaults: "+e.Error())
	}
	return lr, nil
}

// ---- executing operations against a lease ---------------------------------

// withLease locks the repo's lease, loads it, runs fn against the machine,
// delivers the resulting messages and cancels, persists the state when it
// changed, and unlocks. Message delivery happens under the lease mutex so a
// contact can never race a stale check-in onto the wire. Errors from argus
// delivery never fail the operation; they land in the returned warnings.
func withLease(ctx context.Context, client *argus.Client, lr *leaseRepo, fn func(m *leaseMachine)) ([]string, error) {
	unlock := lockLease(lr.SourceRepo)
	defer unlock()

	st, err := loadLease(lr.SourceRepo)
	if err != nil {
		return nil, fmt.Errorf("read dogfood lease: %w", err)
	}
	before, _ := marshalLease(st)

	cache := map[string]Liveness{}
	live := func(id string) Liveness {
		if lv, ok := cache[id]; ok {
			return lv
		}
		lctx, cancel := context.WithTimeout(ctx, leaseArgusTimeout)
		defer cancel()
		lv := Liveness{Class: LiveUnreachable, Err: "no argus client"}
		if client != nil {
			lv = lookupLiveness(lctx, client, id)
		}
		cache[id] = lv
		setLeaseDiag(lr.SourceRepo, func(d *leaseDiag) {
			if lv.Class == LiveUnreachable {
				d.liveness = fmt.Sprintf("liveness lookup for task %s failed: %s", id, lv.Err)
			} else {
				d.liveness = ""
			}
		})
		return lv
	}
	var gitWarnings []string
	m := newLeaseMachine(st, LeaseEnv{
		Now: time.Now().UTC(), Cfg: lr.Timers, RepoName: lr.Name, RepoSlug: lr.Slug,
		DogfoodBranch: lr.Branch, Live: live, Contains: leaseContainment(ctx, lr, &gitWarnings),
	})
	fn(m)

	warnings := deliverLeaseEffects(ctx, client, lr, m)
	warnings = append(warnings, gitWarnings...)

	after, _ := marshalLease(st)
	if string(before) != string(after) || !leaseFileExists(lr.SourceRepo) && (st.Holder != nil || len(st.Queue) > 0) {
		if err := saveLease(lr.SourceRepo, st); err != nil {
			return warnings, fmt.Errorf("persist dogfood lease: %w", err)
		}
	}
	return warnings, nil
}

func leaseFileExists(sourceRepo string) bool {
	p, err := leasePath(sourceRepo)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// deliverLeaseEffects sends the machine's messages and cancels. A failed
// check-in or final warning is undone in the state so it doesn't count as
// silence and is retried on the next evaluation.
func deliverLeaseEffects(ctx context.Context, client *argus.Client, lr *leaseRepo, m *leaseMachine) []string {
	var warnings []string
	if client == nil {
		if len(m.fx.Msgs) > 0 {
			warnings = append(warnings, "argus unreachable: lease messages were not delivered")
		}
		for _, msg := range m.fx.Msgs {
			m.MarkUndelivered(msg)
		}
		return warnings
	}
	// A release caused by a notify 404 queues new messages (the grant), so
	// deliver in rounds.
	for round := 0; round < 4; round++ {
		msgs, cancels := m.fx.Msgs, m.fx.Cancels
		m.fx = LeaseEffects{}
		if len(msgs) == 0 && len(cancels) == 0 {
			break
		}
		for _, c := range cancels {
			cctx, cancel := context.WithTimeout(ctx, leaseArgusTimeout)
			_ = client.CancelNotify(cctx, c.TaskID, c.DeliveryID) // best effort: already delivered or gone
			cancel()
		}
		for _, msg := range msgs {
			nctx, cancel := context.WithTimeout(ctx, leaseArgusTimeout)
			state, err := client.Notify(nctx, msg.TaskID, msg.Text, msg.DeliveryID, msg.Deadline)
			cancel()
			if err == nil {
				m.NoteDeliveryState(msg, state)
				setLeaseDiag(lr.SourceRepo, func(d *leaseDiag) { d.delivery = "" })
				continue
			}
			m.MarkUndelivered(msg)     // checkin/final are retried next tick
			if argus.IsNotFound(err) { // the task is gone: dead, and nothing to warn about
				m.ReleaseDead(msg.TaskID) // no-op unless it is the current holder
				continue
			}
			text := fmt.Sprintf("could not deliver %s message to task %s: %v", msg.Kind, msg.TaskID, err)
			warnings = append(warnings, text)
			setLeaseDiag(lr.SourceRepo, func(d *leaseDiag) { d.delivery = text })
		}
	}
	return warnings
}

// ---- scheduler ------------------------------------------------------------------

// leaseStateFiles lists every persisted lease file under the audit dir.
func leaseStateFiles() []string {
	base, err := AuditDir()
	if err != nil {
		return nil
	}
	matches, _ := filepath.Glob(filepath.Join(base, "repos", "*", LeaseFilename))
	return matches
}

// ActiveLeaseRepos returns the source repos whose persisted lease has a holder
// or a queue. The scheduler discovers leases this way (rather than from an
// in-memory registry) so a claim made by a CLI process is ticked too.
func ActiveLeaseRepos() []string {
	var out []string
	for _, p := range leaseStateFiles() {
		st, err := readLeaseFile(p)
		if err != nil || st.SourceRepo == "" || (st.Holder == nil && len(st.Queue) == 0) {
			continue
		}
		out = append(out, st.SourceRepo)
	}
	return out
}

// TickLease evaluates one repo's lease timers. Cheap when the lease is free.
func TickLease(ctx context.Context, client *argus.Client, sourceRepo string) error {
	lr, err := resolveLeaseRepo(ctx, sourceRepo)
	if err != nil {
		return err
	}
	_, err = withLease(ctx, client, lr, func(m *leaseMachine) { m.Tick() })
	return err
}

// StartupScanLeases applies restart recovery (D4) to every persisted lease
// that has a holder or queue: an iris restart never counts as holder silence.
func StartupScanLeases(ctx context.Context, now time.Time) {
	for _, src := range ActiveLeaseRepos() {
		timers := config.DefaultDogfoodLeaseTimers()
		if lr, err := resolveLeaseRepo(ctx, src); err == nil {
			timers = lr.Timers
		}
		unlock := lockLease(src)
		if cur, err := loadLease(src); err == nil {
			RecoverLeaseAfterRestart(cur, now, timers)
			_ = saveLease(src, cur)
		}
		unlock()
	}
}

// RunLeaseScheduler runs the lease scheduler until ctx is cancelled: a startup
// recovery scan, then a tick over every active lease each interval.
func RunLeaseScheduler(ctx context.Context, client *argus.Client, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	StartupScanLeases(ctx, time.Now().UTC())
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, src := range ActiveLeaseRepos() {
				if ctx.Err() != nil {
					return
				}
				_ = TickLease(ctx, client, src)
			}
		}
	}
}

// ---- verbs ---------------------------------------------------------------

// refResult is the {task_id, task_name} shape in verb results.
type refResult struct {
	TaskID   string `json:"task_id"`
	TaskName string `json:"task_name"`
}

// DogfoodClaimInput is the input for iris:dogfood_claim.
type DogfoodClaimInput struct {
	SHA         string
	TaskID      string
	WaitSeconds int
	Position    int
}

// DogfoodClaimResult is the result of iris:dogfood_claim.
type DogfoodClaimResult struct {
	SHA           string     `json:"sha,omitempty"`
	Status        string     `json:"status"` // granted | queued | already_deployed
	DogfoodBranch string     `json:"dogfood_branch"`
	Holder        *refResult `json:"holder"`
	Position      int        `json:"position"`
	Ahead         []LeaseRef `json:"ahead"`
	QueueLength   int        `json:"queue_length"`
	NextCheckinAt *time.Time `json:"next_checkin_at,omitempty"`
	Message       string     `json:"message"`
	Warnings      []string   `json:"warnings,omitempty"`
}

func leaseTargetRepo(ctx context.Context, client *argus.Client, taskID, path string) (*ResolvedRepo, *leaseRepo, error) {
	target, err := ResolveTarget(ctx, client, taskID, path)
	if err != nil {
		return nil, nil, err
	}
	lr, err := resolveLeaseRepo(ctx, target.SourceRepo)
	if err != nil {
		return nil, nil, err
	}
	return target, lr, nil
}

func taskNameOf(t *ResolvedRepo) string {
	if t != nil && t.Task != nil {
		return t.Task.Name
	}
	return ""
}

// DogfoodClaim claims the dogfood lease or joins the queue, optionally
// waiting up to WaitSeconds (clamped 0-25) for a grant.
func DogfoodClaim(ctx context.Context, client *argus.Client, in DogfoodClaimInput) (*DogfoodClaimResult, error) {
	if in.TaskID == "" {
		return nil, errors.New("task_id is required")
	}
	if in.Position < 0 {
		return nil, errors.New("position must be >= 1")
	}
	target, lr, err := leaseTargetRepo(ctx, client, in.TaskID, "")
	if err != nil {
		return nil, err
	}
	wait := clampLeaseWait(in.WaitSeconds)
	name := taskNameOf(target)
	sha, err := resolveClaimSHA(ctx, lr.SourceRepo, in.SHA)
	if err != nil {
		return nil, err
	}

	var out ClaimOutcome
	var warnings []string
	claim := func(pos int) error {
		w, err := withLease(ctx, client, lr, func(m *leaseMachine) {
			out = m.Claim(in.TaskID, name, pos, sha)
			sha = out.SHA // retain an inherited queued request across removal during this wait
		})
		warnings = append(warnings, w...)
		return err
	}
	if err := claim(in.Position); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(time.Duration(wait) * time.Second)
	for !out.Granted && !out.AlreadyDeployed && wait > 0 && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(leasePollInterval):
		}
		if err := claim(0); err != nil { // re-claim while queued is a poll
			return nil, err
		}
	}
	return claimResult(lr, in.TaskID, out, append(lr.Warnings, dedupe(warnings)...)), nil
}

// clampLeaseWait clamps wait_seconds to 0-25 so a call stays under MCP client timeouts.
func clampLeaseWait(n int) int {
	if n < 0 {
		return 0
	}
	if n > leaseMaxWaitSeconds {
		return leaseMaxWaitSeconds
	}
	return n
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func claimResult(lr *leaseRepo, taskID string, out ClaimOutcome, warnings []string) *DogfoodClaimResult {
	r := &DogfoodClaimResult{
		SHA:           out.SHA,
		DogfoodBranch: lr.Branch,
		Position:      out.Position,
		Ahead:         out.Ahead,
		QueueLength:   out.QueueLength,
		Warnings:      warnings,
	}
	if r.Ahead == nil {
		r.Ahead = []LeaseRef{}
	}
	if out.Holder.TaskID != "" {
		r.Holder = &refResult{TaskID: out.Holder.TaskID, TaskName: out.Holder.TaskName}
	}
	if out.AlreadyDeployed {
		r.Status = "already_deployed"
		r.Message = fmt.Sprintf("Your commit %s is live on %s for %s; no lease is needed. --force or an external branch rewrite can remove it. Confirm branch contents with iris_status.", cleanText(out.SHA), cleanText(lr.Branch), cleanText(lr.Name))
	} else if out.Granted {
		r.Status = "granted"
		r.Position = 0
		t := out.NextCheckinAt
		r.NextCheckinAt = &t
		r.Message = fmt.Sprintf("You hold the dogfood lease for %s (branch %s). Call iris_set_dogfood, then iris_dogfood_release when you're done. A check-in is due at %s; answer it with iris_dogfood_renew.",
			lr.Name, lr.Branch, t.Format(time.RFC3339))
	} else {
		r.Status = "queued"
		holder := "someone"
		if r.Holder != nil {
			holder = fmt.Sprintf("%s (%s)", r.Holder.TaskName, r.Holder.TaskID)
		}
		r.Message = fmt.Sprintf("The dogfood lease for %s is busy: held by %s. You are at position %d. Iris will message you when the lease is granted or your commit is deployed; call iris_dogfood_claim again to poll.",
			lr.Name, holder, out.Position)
	}
	return r
}

// DogfoodRenewResult is the result of iris:dogfood_renew.
type DogfoodRenewResult struct {
	Renewed       bool      `json:"renewed"`
	NextCheckinAt time.Time `json:"next_checkin_at"`
}

// DogfoodRenew is the holder's "still using it" reply. A non-holder gets a
// *LeaseRefusal error.
func DogfoodRenew(ctx context.Context, client *argus.Client, taskID string) (*DogfoodRenewResult, error) {
	if taskID == "" {
		return nil, errors.New("task_id is required")
	}
	_, lr, err := leaseTargetRepo(ctx, client, taskID, "")
	if err != nil {
		return nil, err
	}
	var ref *LeaseRefusal
	var next time.Time
	if _, err := withLease(ctx, client, lr, func(m *leaseMachine) {
		ref = m.Renew(taskID)
		next = m.nextCheckinAt()
	}); err != nil {
		return nil, err
	}
	if ref != nil {
		return nil, ref
	}
	return &DogfoodRenewResult{Renewed: true, NextCheckinAt: next}, nil
}

// DogfoodReleaseResult is the result of iris:dogfood_release.
type DogfoodReleaseResult struct {
	Released  bool   `json:"released"`
	Was       string `json:"was"`
	GrantedTo string `json:"granted_to,omitempty"`
}

// DogfoodRelease releases the lease or leaves the queue.
func DogfoodRelease(ctx context.Context, client *argus.Client, taskID string) (*DogfoodReleaseResult, error) {
	if taskID == "" {
		return nil, errors.New("task_id is required")
	}
	_, lr, err := leaseTargetRepo(ctx, client, taskID, "")
	if err != nil {
		return nil, err
	}
	var out ReleaseOutcome
	if _, err := withLease(ctx, client, lr, func(m *leaseMachine) { out = m.Release(taskID) }); err != nil {
		return nil, err
	}
	return &DogfoodReleaseResult{Released: out.Released, Was: out.Was, GrantedTo: out.GrantedTo}, nil
}

// DogfoodForceReleaseInput is the input for iris:dogfood_force_release.
type DogfoodForceReleaseInput struct {
	TaskID string
	Path   string
	Reason string
}

// DogfoodForceReleaseResult is the result of iris:dogfood_force_release.
type DogfoodForceReleaseResult struct {
	Released  bool     `json:"released"`
	Evicted   string   `json:"evicted,omitempty"`
	GrantedTo string   `json:"granted_to,omitempty"`
	Warnings  []string `json:"warnings,omitempty"`
}

// DogfoodForceRelease evicts the holder regardless of its liveness. It works
// with argus unreachable: messaging is then skipped with a warning.
func DogfoodForceRelease(ctx context.Context, client *argus.Client, in DogfoodForceReleaseInput) (*DogfoodForceReleaseResult, error) {
	if strings.TrimSpace(in.Reason) == "" {
		return nil, errors.New("reason is required")
	}
	lr, resolveWarning, err := forceReleaseTarget(ctx, client, in)
	if err != nil {
		return nil, err
	}
	res := &DogfoodForceReleaseResult{}
	var evicted string
	w, err := withLease(ctx, client, lr, func(m *leaseMachine) {
		if m.st.Holder != nil {
			evicted = m.st.Holder.TaskID
		}
		res.Released, res.GrantedTo = m.ForceRelease(in.Reason)
	})
	if err != nil {
		return nil, err
	}
	if res.Released {
		res.Evicted = evicted
	}
	if resolveWarning != "" {
		w = append([]string{resolveWarning}, w...)
	}
	res.Warnings = dedupe(w)
	return res, nil
}

// forceReleaseTarget resolves the repo for force-release. It prefers the normal
// argus resolution, but an eviction must work with argus down: on failure (or
// no client) it falls back to the persisted lease state — the repo whose lease
// names task_id as holder or queued, or the given path when it has a lease.
func forceReleaseTarget(ctx context.Context, client *argus.Client, in DogfoodForceReleaseInput) (*leaseRepo, string, error) {
	var firstErr error
	if client != nil || (in.TaskID == "" && in.Path == "") {
		_, lr, err := leaseTargetRepo(ctx, client, in.TaskID, in.Path)
		if err == nil {
			return lr, "", nil
		}
		firstErr = err
		if in.TaskID == "" && in.Path == "" {
			return nil, "", err
		}
	}
	src := ""
	switch {
	case in.Path != "":
		if c := canonicalize(in.Path); leaseFileExists(c) {
			src = c
		}
	case in.TaskID != "":
		for _, repo := range ActiveLeaseRepos() {
			if st, err := loadLease(repo); err == nil && leaseNames(st, in.TaskID) {
				src = repo
				break
			}
		}
	}
	if src == "" {
		if firstErr != nil {
			return nil, "", firstErr
		}
		return nil, "", errors.New("argus is unreachable and no persisted dogfood lease names that task or path")
	}
	lr, err := resolveLeaseRepo(ctx, src)
	if err != nil {
		return nil, "", err
	}
	return lr, "argus unreachable: target resolved from the persisted lease state", nil
}

func leaseNames(st *LeaseState, taskID string) bool {
	if st.Holder != nil && st.Holder.TaskID == taskID {
		return true
	}
	for _, q := range st.Queue {
		if q.TaskID == taskID {
			return true
		}
	}
	return false
}

// DogfoodQueueMoveInput is the input for iris:dogfood_queue_move.
type DogfoodQueueMoveInput struct {
	TaskID     string
	Path       string
	MoveTaskID string
	Position   int
}

// DogfoodQueueMoveResult is the result of iris:dogfood_queue_move.
type DogfoodQueueMoveResult struct {
	Queue []LeaseRef `json:"queue"`
}

// DogfoodQueueMove reorders the queue. It sends no messages.
func DogfoodQueueMove(ctx context.Context, client *argus.Client, in DogfoodQueueMoveInput) (*DogfoodQueueMoveResult, error) {
	if in.MoveTaskID == "" {
		return nil, errors.New("move_task_id is required")
	}
	if in.Position < 1 {
		in.Position = 1 // clamped to [1, queue_length]
	}
	_, lr, err := leaseTargetRepo(ctx, client, in.TaskID, in.Path)
	if err != nil {
		return nil, err
	}
	var q []LeaseRef
	var merr error
	if _, err := withLease(ctx, client, lr, func(m *leaseMachine) { q, merr = m.QueueMove(in.MoveTaskID, in.Position) }); err != nil {
		return nil, err
	}
	if merr != nil {
		return nil, merr
	}
	return &DogfoodQueueMoveResult{Queue: q}, nil
}

// ---- set_dogfood gate -------------------------------------------------------

// LeaseGateError is returned by SetDogfood when the caller does not hold the
// lease and did not pass lease_override. The structured fields let agents
// avoid parsing the text.
type LeaseGateError struct {
	Message  string
	Holder   *LeaseRef
	Ahead    []LeaseRef
	Position int
}

func (e *LeaseGateError) Error() string { return e.Message }

func newLeaseGateError(r *LeaseRefusal) *LeaseGateError {
	var b strings.Builder
	b.WriteString("refusing to deploy: " + r.holderText())
	if len(r.Ahead) > 0 {
		b.WriteString("; queued ahead of you: " + refList(r.Ahead))
	}
	if r.Position > 0 {
		fmt.Fprintf(&b, "; you are at queue position %d", r.Position)
	}
	b.WriteString(". Call iris_dogfood_claim to take (or queue for) the lease, or pass lease_override=true to deploy anyway.")
	return &LeaseGateError{Message: b.String(), Holder: r.Holder, Ahead: r.Ahead, Position: r.Position}
}

// leaseGate is an entered set_dogfood gate.
type leaseGate struct {
	ctx      context.Context
	client   *argus.Client
	lr       *leaseRepo
	taskID   string
	holding  bool // caller is the holder: finish() ends the in-flight call
	override *LeaseRef
	// overrideWarning, when set, goes first in warnings.
	overrideWarning string
}

// enterLeaseGate runs the set_dogfood lease gate (spec: "Dogfood lease gate").
// internal marks iris-internal callers (the ship recompose), which pass
// without an override when the lease is free or held by taskID.
func enterLeaseGate(ctx context.Context, client *argus.Client, lr *leaseRepo, taskID string, override, internal bool) (*leaseGate, error) {
	g := &leaseGate{ctx: ctx, client: client, lr: lr, taskID: taskID}
	var refusal *LeaseRefusal
	_, err := withLease(ctx, client, lr, func(m *leaseMachine) {
		switch {
		case taskID != "" && m.st.Holder != nil && m.st.Holder.TaskID == taskID:
			m.BeginFlight(taskID)
			g.holding = true
		case internal && m.st.Holder == nil:
			// Free lease: the internal caller passes without holding it.
		case override:
			if m.st.Holder != nil {
				h := m.holderRef()
				g.override = &h
				g.overrideWarning = fmt.Sprintf("LEASE OVERRIDE: deployed while %s (%s) holds the dogfood lease; the holder was notified.", h.TaskName, h.TaskID)
			} else {
				g.overrideWarning = "LEASE OVERRIDE: deployed without holding the dogfood lease (no holder)."
			}
		default:
			refusal = m.Refusal(taskID)
		}
	})
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		return nil, newLeaseGateError(refusal)
	}
	return g, nil
}

// finish ends the holder's in-flight call (counts as contact), whatever the
// deploy's outcome.
func (g *leaseGate) finish() {
	if !g.holding {
		return
	}
	_, _ = withLease(g.ctx, g.client, g.lr, func(m *leaseMachine) { m.EndFlight(g.taskID) })
}

// notifyOverridden tells the holder its dogfood tree was replaced.
func (g *leaseGate) notifyOverridden(callerName, sha string) {
	if g.override == nil || g.override.TaskID == g.taskID {
		return
	}
	caller := callerName
	if caller == "" {
		caller = "a caller with no task"
	}
	text := fmt.Sprintf("[iris] %s deployed to dogfood for %s with lease_override while you hold the lease; the running tree is now %s. Call iris_set_dogfood again to redeploy your build, or iris_dogfood_release if you are done.", cleanText(caller), cleanText(g.lr.Name), cleanText(sha))
	_, _ = withLease(g.ctx, g.client, g.lr, func(m *leaseMachine) {
		if m.st.Holder != nil && m.st.Holder.TaskID == g.override.TaskID {
			m.Notice(g.override.TaskID, leaseMsgOverride, text)
		}
	})
}

// ---- ship_feature recompose check ---------------------------------------------

// leaseHolderRef reports the current holder (nil when free) without side effects.
func leaseHolderRef(sourceRepo string) (*LeaseRef, error) {
	st, err := loadLease(sourceRepo)
	if err != nil {
		return nil, err
	}
	if st.Holder == nil {
		return nil, nil
	}
	return &LeaseRef{TaskID: st.Holder.TaskID, TaskName: st.Holder.TaskName}, nil
}

// noticeHolder queues a notice to the current holder when it is holderID.
func noticeHolder(ctx context.Context, client *argus.Client, sourceRepo, holderID, kind, text string) {
	lr, err := resolveLeaseRepo(ctx, sourceRepo)
	if err != nil {
		return
	}
	_, _ = withLease(ctx, client, lr, func(m *leaseMachine) {
		if m.st.Holder != nil && m.st.Holder.TaskID == holderID {
			m.Notice(holderID, kind, text)
		}
	})
}

// ---- status view -----------------------------------------------------------------

// DogfoodLeaseView is the dogfood_lease field of iris:status.
type DogfoodLeaseView struct {
	Holder   *LeaseHolderView    `json:"holder"`
	Queue    []LeaseQueueView    `json:"queue"`
	Timers   LeaseTimersView     `json:"timers"`
	History  []LeaseHistoryEntry `json:"history"`
	Warnings []string            `json:"warnings"`
}

// LeaseHolderView is the holder as reported by status.
type LeaseHolderView struct {
	TaskID                  string     `json:"task_id"`
	TaskName                string     `json:"task_name"`
	GrantedAt               time.Time  `json:"granted_at"`
	LastContactAt           time.Time  `json:"last_contact_at"`
	NextCheckinAt           time.Time  `json:"next_checkin_at"`
	CheckinOutstandingSince *time.Time `json:"checkin_outstanding_since,omitempty"`
	ReleaseAt               *time.Time `json:"release_at,omitempty"`
	InFlightSetDogfood      bool       `json:"in_flight_set_dogfood"`
}

// LeaseQueueView is a queued task as reported by status.
type LeaseQueueView struct {
	TaskID     string    `json:"task_id"`
	TaskName   string    `json:"task_name"`
	Position   int       `json:"position"`
	EnqueuedAt time.Time `json:"enqueued_at"`
}

// LeaseTimersView reports the effective timers.
type LeaseTimersView struct {
	WarmerSeconds      int        `json:"warmer_seconds"`
	GraceSeconds       int        `json:"grace_seconds"`
	IdleReleaseSeconds int        `json:"idle_release_seconds"`
	InFlightMaxSeconds int        `json:"in_flight_max_seconds"`
	IdleReleaseAt      *time.Time `json:"idle_release_at,omitempty"`
}

// ReadDogfoodLeaseView builds the status field. Read-only: no messages, no
// timer changes, no liveness lookups, no writes. A malformed lease file is
// reported as a warning, never an error.
func ReadDogfoodLeaseView(ctx context.Context, sourceRepo string) *DogfoodLeaseView {
	branch, block, _, err := resolveDogfoodConfig(ctx, sourceRepo)
	if err != nil || branch == "" {
		return nil
	}
	timers, verrs := config.ValidDogfoodLeaseTimers(block)
	v := &DogfoodLeaseView{
		Queue:    []LeaseQueueView{},
		History:  []LeaseHistoryEntry{},
		Warnings: []string{},
		Timers: LeaseTimersView{
			WarmerSeconds: timers.WarmerSeconds, GraceSeconds: timers.GraceSeconds, IdleReleaseSeconds: timers.IdleReleaseSeconds, InFlightMaxSeconds: timers.InFlightMaxSeconds,
		},
	}
	for _, e := range verrs {
		v.Warnings = append(v.Warnings, "invalid [dogfood_lease] config, using defaults: "+e.Error())
	}
	st, err := loadLease(sourceRepo)
	if err != nil {
		p, _ := leasePath(sourceRepo)
		v.Warnings = append(v.Warnings, fmt.Sprintf("unreadable %s: %v", filepath.Base(p), err))
		return v
	}
	if h := st.Holder; h != nil {
		hv := &LeaseHolderView{
			TaskID: h.TaskID, TaskName: h.TaskName, GrantedAt: h.GrantedAt, LastContactAt: h.LastContactAt,
			NextCheckinAt:      h.LastContactAt.Add(time.Duration(timers.WarmerSeconds) * time.Second),
			InFlightSetDogfood: h.InFlight > 0,
		}
		if !h.CheckinSentAt.IsZero() {
			t := h.CheckinSentAt
			hv.CheckinOutstandingSince = &t
			if h.CheckinDeliveryState == "pending" {
				v.Warnings = append(v.Warnings, fmt.Sprintf("the check-in to task %s is accepted by argus but not yet submitted to its session (the session is not idle); it appears when the session next goes idle", h.TaskID))
			}
		}
		if !h.ReleaseAt.IsZero() {
			t := h.ReleaseAt
			hv.ReleaseAt = &t
		}
		v.Holder = hv
		idleAt := h.LastContactAt.Add(time.Duration(timers.IdleReleaseSeconds) * time.Second)
		v.Timers.IdleReleaseAt = &idleAt
	}
	for i, q := range st.Queue {
		v.Queue = append(v.Queue, LeaseQueueView{TaskID: q.TaskID, TaskName: q.TaskName, Position: i + 1, EnqueuedAt: q.EnqueuedAt})
	}
	if st.History != nil {
		v.History = st.History
	}
	d := getLeaseDiag(sourceRepo)
	if d.liveness != "" {
		v.Warnings = append(v.Warnings, d.liveness)
	}
	if d.delivery != "" {
		v.Warnings = append(v.Warnings, d.delivery)
	}
	return v
}

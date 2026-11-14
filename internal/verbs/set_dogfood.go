// Package verbs: set_dogfood implements iris:set_dogfood — atomically point
// the configured dogfood branch at a worker-composed SHA, persist a structured
// manifest describing what that SHA contains, and rebuild/restart the service.
//
// "Iris is dumb, agent is smart": composition (cherry-pick vs merge, conflict
// resolution) happens in the agent, which hands iris a finished SHA. Iris only
// moves the ref, records the manifest, and reloads.
//
// See openspec/changes/add-dogfood-and-ship-verbs/design.md and
// specs/iris-set-dogfood/spec.md.

package verbs

import (
	"context"
	"fmt"
	"strings"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
)

// SetDogfoodOpts captures the typed arguments for SetDogfood.
type SetDogfoodOpts struct {
	// compose is used by DogfoodDeploy; Ref resolution and merging stay under the source lock.
	compose bool
	Caller  string
	// Sha is the full commit SHA to point the dogfood branch at. Must be
	// reachable in the source repo's object database.
	Sha string
	// Manifest is the agent-supplied record of what composes Sha. Iris stamps
	// RecordedAt at write time; the rest is descriptive and not validated.
	Manifest *DogfoodManifest
	// Force overrides the commit-dropping ancestry refusal. When false (the
	// default), a Sha that is not a descendant of the current dogfood SHA is
	// refused because deploying it would drop commits. When true, the deploy
	// proceeds and a prominent warning is surfaced. Force relaxes ONLY the
	// ancestry refusal.
	Force bool
	// LeaseOverride deploys even though the caller does not hold the dogfood
	// lease. It is DISTINCT from Force: it relaxes ONLY the lease gate, and
	// Force relaxes ONLY the commit-dropping ancestry refusal.
	LeaseOverride bool
	// InternalCaller marks iris-internal callers (the ship_feature recompose,
	// which has already checked the lease). Such a caller passes the gate when
	// the lease is free or held by taskID, without a LEASE OVERRIDE warning.
	// Never set from an MCP/CLI handler.
	InternalCaller bool
}

// SetDogfoodResult is the structured success payload, mirrored to MCP/CLI as
// pretty-printed JSON.
type SetDogfoodResult struct {
	DogfoodLease  *DogfoodLeaseView `json:"dogfood_lease,omitempty"`
	Set           bool              `json:"set"`
	DogfoodBranch string            `json:"dogfood_branch"`
	PreviousSHA   string            `json:"previous_sha"`
	NewSHA        string            `json:"new_sha"`
	Reload        *ReloadResult     `json:"reload,omitempty"`
	Warnings      []string          `json:"warnings"`
	Message       string            `json:"message"`
}

// SetDogfood atomically hard-resets the configured dogfood branch to opts.Sha,
// persists opts.Manifest alongside the audit log, and runs the existing
// build/restart machinery (iris:reload).
//
// Ordering and safety:
//   - Config + SHA reachability are checked before any lock or mutation. A repo
//     without dogfood_branch declared is refused with no side effects.
//   - The manifest is written BEFORE the branch reset (durable-first). If the
//     write fails, no git mutation occurs. If the reset then fails, the manifest
//     is "ahead" of the branch — visible as drift on the next iris:status.
//   - The per-source-repo lock spans composition, manifest write, branch reset,
//     reload and readiness so concurrent deploys cannot change the served tree.
func SetDogfood(ctx context.Context, client *argus.Client, taskID string, opts SetDogfoodOpts) (*SetDogfoodResult, error) {
	result, err := setDogfood(ctx, client, taskID, opts)
	if result != nil && result.Reload != nil {
		result.DogfoodLease = ReadDogfoodLeaseView(context.WithoutCancel(ctx), result.Reload.TargetSourceRepo)
	}
	// All deferred lock, scratch-branch and lease cleanup has finished before
	// scheduling a daemon exit. Otherwise a slow lease reconciliation can be
	// interrupted by the 100ms exit timer, leaving a persisted flight behind.
	if result != nil && result.Reload != nil && result.Reload.restartCode != nil {
		scheduleSelfExit(*result.Reload.restartCode)
	}
	return result, err
}

func setDogfood(ctx context.Context, client *argus.Client, taskID string, opts SetDogfoodOpts) (*SetDogfoodResult, error) {
	if opts.Sha == "" {
		return nil, fmt.Errorf("sha is required")
	}
	if strings.HasPrefix(opts.Sha, "-") {
		return nil, fmt.Errorf("invalid sha %q (must not begin with '-')", opts.Sha)
	}
	if opts.Manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}

	// 1. Resolve target (no side effects, no lock).
	target, err := ResolveTarget(ctx, client, taskID, "")
	if err != nil {
		return nil, err
	}

	// 2. Require dogfood_branch config, resolved from the MERGED overlay
	// (.iris.toml + the optional gitignored .iris.local.toml). dogfood_branch
	// is a local-tagged field, so it normally lives in .iris.local.toml; a
	// shared-only read would miss it (and disagree with iris:validate_config,
	// which is overlay-aware). A missing .iris.toml is treated the same as a
	// merged config that doesn't declare the field.
	isSelf, _ := isSelfTarget(ctx, target.SourceRepo)
	if isSelf && opts.Caller == "cli" {
		return nil, ErrCLISelfReloadUnsupported
	}
	overlay, err := config.LoadOverlay(target.SourceRepo, isSelf)
	if err != nil {
		return nil, err
	}
	dogfoodBranch := ""
	if overlay.Doc != nil {
		dogfoodBranch = overlay.Doc.DogfoodBranch
	}
	if dogfoodBranch == "" {
		// Bootstrap: `.iris.toml` may be absent at the source root (it lives on
		// the dogfood branch we're about to build), in which case LoadOverlay
		// yields no doc and the local overlay's dogfood_branch is unseen. But
		// dogfood_branch IS the pointer to that branch, so read it directly
		// from `.iris.local.toml` — its resolution must not be gated behind
		// `.iris.toml` existing.
		dogfoodBranch = config.PeekLocalDogfoodBranch(target.SourceRepo)
	}
	if dogfoodBranch == "" {
		return nil, fmt.Errorf(`dogfood_branch not configured for this repo (add dogfood_branch = "..." to .iris.local.toml)`)
	}

	// Surface overlay taxonomy warnings (e.g. a local-tagged field left in
	// .iris.toml) to the caller.
	overlayWarnings := []string{}
	for _, w := range overlay.Warnings {
		if w.Hint != "" {
			overlayWarnings = append(overlayWarnings, fmt.Sprintf("%s (%s)", w.Message, w.Hint))
		} else {
			overlayWarnings = append(overlayWarnings, w.Message)
		}
	}

	// 3. Verify the SHA resolves to a commit reachable in the source repo.
	out, err := runGit(ctx, target.SourceRepo, "rev-parse", "--verify", "--quiet", opts.Sha+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("sha %q is not reachable in %s", opts.Sha, target.SourceRepo)
	}
	newSHA := strings.TrimSpace(out)
	if opts.Manifest.Base.SHA == "" {
		opts.Manifest.Base.SHA = newSHA
	}

	// 3b. Dogfood lease gate: after config + reachability, before the ancestry
	// check, any git mutation, the manifest write or the reload — so a refusal
	// has no side effects. A holder's call counts as contact at its start and
	// (via finish) its end, and the holder is not released while it runs.
	lr, err := resolveLeaseRepo(ctx, target.SourceRepo)
	if err != nil {
		return nil, err
	}
	gate, err := enterLeaseGate(ctx, client, lr, taskID, opts.LeaseOverride, opts.InternalCaller)
	if err != nil {
		return nil, err
	}
	defer gate.finish()

	// 4. Hold the source lock through ref movement, build, restart and readiness.
	mu := lockSourceRepo(target.SourceRepo)
	lockHeld := true
	releaseLock := func() {
		if lockHeld {
			mu.Unlock()
			lockHeld = false
		}
	}
	defer releaseLock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx = context.WithoutCancel(ctx)
	MarkRunStarted(ctx)
	if err := checkCleanTree(ctx, target.SourceRepo); err != nil {
		return nil, err
	}
	// Refuse a dogfood branch checked out in another worktree: deployments must
	// operate on the host, never reset a worker's tree.
	if at, err := worktreeForBranch(ctx, target.SourceRepo, dogfoodBranch); err != nil {
		return nil, err
	} else if at != "" && canonicalize(at) != canonicalize(target.SourceRepo) {
		return nil, fmt.Errorf("dogfood branch %s is checked out in another worktree: %s", dogfoodBranch, at)
	}
	// 5. Prior dogfood-branch SHA ("" if the branch doesn't exist yet).
	previousSHA := ""
	if prev, err := runGit(ctx, target.SourceRepo, "rev-parse", "--verify", "--quiet", "refs/heads/"+dogfoodBranch); err == nil {
		previousSHA = strings.TrimSpace(prev)
	}
	stateDir, err := SourceRepoStateDir(target.SourceRepo)
	if err != nil {
		return nil, fmt.Errorf("resolve state dir: %w", err)
	}

	if opts.compose {
		base := previousSHA
		if base == "" {
			base, err = runDeploymentGit(ctx, target.SourceRepo, "rev-parse", "HEAD")
			if err != nil {
				return nil, err
			}
			base = strings.TrimSpace(base)
		}
		prior, err := ReadManifest(stateDir)
		if err != nil {
			return nil, fmt.Errorf("read composition manifest: %w", err)
		}
		composed, cleanup, err := composeDogfood(ctx, target.SourceRepo, base, newSHA)
		if err != nil {
			return nil, err
		}
		defer cleanup()
		opts.Manifest = &DogfoodManifest{Base: ManifestBase{Ref: dogfoodBranch, SHA: base}, Note: "Composed onto existing dogfood tip"}
		if prior != nil {
			opts.Manifest.Base = prior.Base
			opts.Manifest.Layered = append([]LayeredEntry(nil), prior.Layered...)
		}
		opts.Manifest.Layered = append(opts.Manifest.Layered, LayeredEntry{Name: opts.Sha, SHA: newSHA, Applied: "merge"})
		newSHA = composed
	}

	// 5b. Ancestry safety. When the dogfood branch already exists and newSHA is
	// NOT a descendant of its current SHA, deploying newSHA drops every commit
	// reachable from previousSHA but not from newSHA. Refuse (naming the count
	// and previousSHA) unless Force is set; on Force, proceed with a prominent
	// warning. Iris does NOT recompose — that is the agent's job. This runs
	// before the manifest write so a refusal has no side effects.
	forceWarning := ""
	if previousSHA != "" {
		if _, err := runGit(ctx, target.SourceRepo, "merge-base", "--is-ancestor", previousSHA, newSHA); err != nil {
			dropped := droppedCommitCount(ctx, target.SourceRepo, newSHA, previousSHA)
			if !opts.Force {
				return nil, fmt.Errorf(
					"refusing to move dogfood branch %s to %s: it is not a descendant of the current dogfood SHA %s and would drop %s commit(s). Recompose onto %s, or pass force=true to override.",
					dogfoodBranch, newSHA, previousSHA, dropped, previousSHA)
			}
			forceWarning = fmt.Sprintf(
				"FORCED commit-dropping deploy: %s is not a descendant of the previous dogfood SHA %s; %s commit(s) reachable from %s were dropped.",
				newSHA, previousSHA, dropped, previousSHA)
		}
	}

	// 6. Persist the manifest FIRST (durable-first). On failure, no git mutation.
	if err := WriteManifest(stateDir, opts.Manifest); err != nil {
		return nil, fmt.Errorf("persist manifest: %w", err)
	}

	// 7. Move the dogfood ref (worktree-guarded).
	//   - Create when missing (a nonexistent branch cannot be checked out).
	//   - Otherwise force-move. `git branch -f` refuses a branch that is checked
	//     out in any worktree ("cannot force update the branch used by
	//     worktree"), which is the normal state on a live dogfood host. When the
	//     branch IS checked out, `git reset --hard` in that worktree instead, so
	//     the ref, HEAD, index, and tree all move together. We do NOT use
	//     `git update-ref`: it would move the ref out from under the checked-out
	//     worktree, leaving HEAD/index disagreeing with the tree (phantom diffs).
	if previousSHA == "" {
		if out, err := runGit(ctx, target.SourceRepo, "branch", dogfoodBranch, newSHA); err != nil {
			return nil, fmt.Errorf("create dogfood branch %s at %s: %w; log:\n%s", dogfoodBranch, newSHA, err, out)
		}
	} else {
		checkedOutAt, err := worktreeForBranch(ctx, target.SourceRepo, dogfoodBranch)
		if err != nil {
			return nil, fmt.Errorf("inspect worktrees for dogfood branch %s: %w", dogfoodBranch, err)
		}
		if checkedOutAt == "" {
			if out, err := runGit(ctx, target.SourceRepo, "branch", "-f", dogfoodBranch, newSHA); err != nil {
				return nil, fmt.Errorf("reset dogfood branch %s to %s: %w; log:\n%s", dogfoodBranch, newSHA, err, out)
			}
		} else {
			if out, err := runGit(ctx, checkedOutAt, "reset", "--hard", newSHA); err != nil {
				return nil, fmt.Errorf("reset checked-out dogfood branch %s to %s in %s: %w; log:\n%s", dogfoodBranch, newSHA, checkedOutAt, err, out)
			}
		}
	}

	// Select the resting branch before loading the build configuration or
	// restarting. In particular, never bounce on main and switch afterwards.
	if out, err := runDeploymentGit(ctx, target.SourceRepo, "checkout", dogfoodBranch); err != nil {
		return nil, fmt.Errorf("checkout dogfood %s: %w; %s", dogfoodBranch, err, out)
	}

	// 8. Keep the lock through reload to prevent a concurrent deploy changing
	// the checkout between branch movement and restart.
	// A moved branch may obsolete pending success notices even if reload fails.
	leaseWarnings, leaseErr := withLease(ctx, client, lr, func(m *leaseMachine) { m.CancelInvalidDeployments() })
	if leaseErr != nil {
		leaseWarnings = append(leaseWarnings, "cancel obsolete deployed notices: "+leaseErr.Error())
	}

	gate.notifyOverridden(taskNameOf(target), newSHA)
	warnings := []string{}
	if gate.overrideWarning != "" {
		warnings = append(warnings, gate.overrideWarning)
	}
	if forceWarning != "" {
		warnings = append(warnings, forceWarning)
	}
	warnings = append(warnings, overlayWarnings...)
	warnings = append(warnings, leaseWarnings...)

	// 9. Rebuild + restart via the existing reload sequence. NoPull: the
	// origin-first model keeps the default branch read-only (it moves only via
	// explicit iris:fetch); set_dogfood builds the SHA we just composed rather
	// than pulling new upstream work as a side effect. BuildBranch: reload
	// selects dogfood for the build+restart and leaves it selected afterward.
	caller := taskID
	if opts.Caller != "" {
		caller = opts.Caller
	}
	if caller == "" {
		caller = "self"
	}
	reload, err := Reload(ctx, client, ReloadInput{
		TaskID:        taskID,
		NoPull:        true,
		Caller:        caller,
		BuildBranch:   dogfoodBranch,
		lockHeld:      true,
		deferSelfExit: true,
		target:        target,
	})
	if reload != nil {
		warnings = append(warnings, reload.Warnings...)
	}
	if err != nil {
		return &SetDogfoodResult{Set: true, DogfoodBranch: dogfoodBranch, PreviousSHA: previousSHA, NewSHA: newSHA, Reload: reload, Warnings: warnings}, fmt.Errorf("reload after set_dogfood: %w", err)
	}

	w, reconcileErr := withLease(ctx, client, lr, func(m *leaseMachine) { m.ReconcileQueue() })
	warnings = append(warnings, w...)
	if reconcileErr != nil {
		warnings = append(warnings, "reconcile dogfood queue: "+reconcileErr.Error())
	}

	return &SetDogfoodResult{
		Set:           true,
		DogfoodBranch: dogfoodBranch,
		PreviousSHA:   previousSHA,
		NewSHA:        newSHA,
		Reload:        reload,
		Warnings:      warnings,
		Message:       DogfoodReleaseGuidance,
	}, nil
}

// worktreeForBranch reports the working-tree path where branch is currently
// checked out in sourceRepo, or "" if it is not checked out in any worktree.
// It parses `git worktree list --porcelain`, whose per-worktree records are a
// `worktree <path>` line followed by a `branch refs/heads/<name>` line (absent
// for detached HEADs), separated by blank lines.
func worktreeForBranch(ctx context.Context, sourceRepo, branch string) (string, error) {
	out, err := runGit(ctx, sourceRepo, "worktree", "list", "--porcelain")
	if err != nil {
		return "", err
	}
	want := "branch refs/heads/" + branch
	curPath := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "worktree "):
			curPath = strings.TrimPrefix(line, "worktree ")
		case line == want:
			return curPath, nil
		}
	}
	return "", nil
}

// droppedCommitCount returns the number of commits reachable from previousSHA
// but not from newSHA (`git rev-list --count <newSHA>..<previousSHA>`) as a
// string, or "?" if the count cannot be determined. Used only for the
// commit-dropping refusal/warning message.
func droppedCommitCount(ctx context.Context, sourceRepo, newSHA, previousSHA string) string {
	if out, err := runGit(ctx, sourceRepo, "rev-list", "--count", newSHA+".."+previousSHA); err == nil {
		return strings.TrimSpace(out)
	}
	return "?"
}

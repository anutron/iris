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
	// Sha is the full commit SHA to point the dogfood branch at. Must be
	// reachable in the source repo's object database.
	Sha string
	// Manifest is the agent-supplied record of what composes Sha. Iris stamps
	// RecordedAt at write time; the rest is descriptive and not validated.
	Manifest *DogfoodManifest
}

// SetDogfoodResult is the structured success payload, mirrored to MCP/CLI as
// pretty-printed JSON.
type SetDogfoodResult struct {
	Set           bool          `json:"set"`
	DogfoodBranch string        `json:"dogfood_branch"`
	PreviousSHA   string        `json:"previous_sha"`
	NewSHA        string        `json:"new_sha"`
	Reload        *ReloadResult `json:"reload,omitempty"`
	Warnings      []string      `json:"warnings"`
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
//   - The per-source-repo lock is held across the manifest write + branch reset
//     so concurrent set_dogfood calls serialize. It is released before the
//     reload, which re-acquires the same lock (the mutex is not reentrant).
func SetDogfood(ctx context.Context, client *argus.Client, taskID string, opts SetDogfoodOpts) (*SetDogfoodResult, error) {
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

	// 4. Acquire the source-repo lock for the atomic manifest-write + reset.
	mu := lockSourceRepo(target.SourceRepo)
	lockHeld := true
	releaseLock := func() {
		if lockHeld {
			mu.Unlock()
			lockHeld = false
		}
	}
	defer releaseLock()

	// 5. Prior dogfood-branch SHA ("" if the branch doesn't exist yet).
	previousSHA := ""
	if prev, err := runGit(ctx, target.SourceRepo, "rev-parse", "--verify", "--quiet", "refs/heads/"+dogfoodBranch); err == nil {
		previousSHA = strings.TrimSpace(prev)
	}

	// 6. Persist the manifest FIRST (durable-first). On failure, no git mutation.
	stateDir, err := SourceRepoStateDir(target.SourceRepo)
	if err != nil {
		return nil, fmt.Errorf("resolve state dir: %w", err)
	}
	if err := WriteManifest(stateDir, opts.Manifest); err != nil {
		return nil, fmt.Errorf("persist manifest: %w", err)
	}

	// 7. Move the dogfood ref. Create when missing, force-move otherwise. We do
	// not check the branch out — iris only repositions the ref.
	if previousSHA == "" {
		if out, err := runGit(ctx, target.SourceRepo, "branch", dogfoodBranch, newSHA); err != nil {
			return nil, fmt.Errorf("create dogfood branch %s at %s: %w; log:\n%s", dogfoodBranch, newSHA, err, out)
		}
	} else {
		if out, err := runGit(ctx, target.SourceRepo, "branch", "-f", dogfoodBranch, newSHA); err != nil {
			return nil, fmt.Errorf("reset dogfood branch %s to %s: %w; log:\n%s", dogfoodBranch, newSHA, err, out)
		}
	}

	// 8. Release the lock before reload (Reload re-acquires the same mutex).
	releaseLock()

	// 9. Rebuild + restart via the existing reload sequence. NoPull: the
	// origin-first model keeps the default branch read-only (it moves only via
	// explicit iris:fetch); set_dogfood builds the SHA we just composed rather
	// than pulling new upstream work as a side effect. BuildBranch: reload
	// checks out the dogfood branch for the build+restart (so the composed SHA
	// is what gets built and deployed) and restores the default branch after.
	caller := taskID
	if caller == "" {
		caller = "self"
	}
	reload, err := Reload(ctx, client, ReloadInput{
		TaskID:      taskID,
		NoPull:      true,
		Caller:      caller,
		BuildBranch: dogfoodBranch,
	})
	if err != nil {
		return nil, fmt.Errorf("reload after set_dogfood: %w", err)
	}

	warnings := append([]string{}, overlayWarnings...)
	if reload != nil && len(reload.Warnings) > 0 {
		warnings = append(warnings, reload.Warnings...)
	}

	return &SetDogfoodResult{
		Set:           true,
		DogfoodBranch: dogfoodBranch,
		PreviousSHA:   previousSHA,
		NewSHA:        newSHA,
		Reload:        reload,
		Warnings:      warnings,
	}, nil
}

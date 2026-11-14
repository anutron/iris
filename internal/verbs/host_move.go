package verbs

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// hostMoveGate is the dogfood-lease gate for verbs that deliberately move or
// modify the host's working branch (checkout, cherry_pick, merge_to_master,
// complete_task).
//
// A move "changes what dogfood serves" when the host is currently on the
// configured dogfood_branch and the verb would leave it for another branch
// (target), or when the verb modifies the dogfood_branch ref itself
// (modifiesTarget && target == dogfood_branch). Only then is the lease
// consulted: free or caller-held passes; another holder refuses with a
// *LeaseGateError unless override is set, which returns a LEASE OVERRIDE
// warning and notifies the holder. Repos with no dogfood_branch, and any
// config/lease read failure that cannot show a conflict, pass: this gate
// protects a running dogfood server, it must not break repos that do not
// have one.
//
// Call it BEFORE lockSourceRepo, matching iris_set_dogfood's ordering.
func hostMoveGate(ctx context.Context, client *argus.Client, resolved *ResolvedRepo, taskID, target string, modifiesTarget, override bool) ([]string, error) {
	branch, _, _, err := resolveDogfoodConfig(ctx, resolved.SourceRepo)
	if err != nil || branch == "" {
		return nil, nil
	}
	cur, err := currentBranch(ctx, resolved.SourceRepo)
	if err != nil {
		return nil, nil
	}
	leavesDogfood := cur == branch && target != branch
	modifiesDogfood := modifiesTarget && target == branch
	if !leavesDogfood && !modifiesDogfood {
		return nil, nil
	}

	holder, err := leaseHolderRef(resolved.SourceRepo)
	if err != nil {
		return nil, fmt.Errorf("read dogfood lease: %w", err)
	}
	if holder == nil || holder.TaskID == taskID {
		return nil, nil
	}

	if !override {
		return nil, &LeaseGateError{
			Holder: holder,
			Message: fmt.Sprintf("refusing to change the dogfood host checkout (on %q, dogfood_branch %q): %s holds the dogfood lease. Call iris_dogfood_claim to take (or queue for) the lease, or pass lease_override=true to proceed anyway.",
				cur, branch, holderText(holder)),
		}
	}

	caller := taskNameOf(resolved)
	if caller == "" {
		caller = taskID
	}
	noticeHolder(ctx, client, resolved.SourceRepo, holder.TaskID, leaseMsgOverride, fmt.Sprintf(
		"[iris] %s moved the %s host checkout off %s with lease_override while you hold the dogfood lease. Re-check what dogfood is serving, then call iris_set_dogfood or iris_dogfood_release.",
		cleanText(caller), cleanText(filepath.Base(resolved.SourceRepo)), cleanText(branch)))
	return []string{fmt.Sprintf("LEASE OVERRIDE: changed the dogfood host checkout while %s holds the dogfood lease; the holder was notified.", holderText(holder))}, nil
}

func holderText(h *LeaseRef) string {
	return fmt.Sprintf("%s (%s)", h.TaskName, h.TaskID)
}

// syncDefaultBranchNoCheckout brings the local default branch up to date with
// origin/<default> WITHOUT ever checking out another branch, so a host serving
// a dogfood branch is not disturbed. The caller holds lockSourceRepo.
//
//   - host on the default branch: `merge --ff-only origin/<default>`.
//   - host elsewhere: fast-forward only the local default ref (update-ref,
//     old-value guarded), when it is an ancestor of origin/<default> and not
//     checked out in any worktree.
//
// All problems are returned as warnings; a missing local default ref is not
// one (there is nothing to update).
func syncDefaultBranchNoCheckout(ctx context.Context, sourceRepo, defaultBranch string) []string {
	var warns []string
	timeout := gitTransferTimeout(sourceRepo)
	if _, err := runGitTransfer(ctx, sourceRepo, timeout, "fetch", "origin", defaultBranch); err != nil {
		return []string{fmt.Sprintf("post-merge: fetch origin %s failed: %v", defaultBranch, err)}
	}
	remoteRef := "refs/remotes/origin/" + defaultBranch
	localRef := "refs/heads/" + defaultBranch
	remoteSHA, err := runGit(ctx, sourceRepo, "rev-parse", "--verify", "--quiet", remoteRef)
	if err != nil {
		return []string{fmt.Sprintf("post-merge: could not resolve %s: %v", remoteRef, err)}
	}
	remoteSHA = strings.TrimSpace(remoteSHA)

	cur, err := currentBranch(ctx, sourceRepo)
	if err != nil {
		return []string{fmt.Sprintf("post-merge: could not read current branch: %v", err)}
	}

	if cur == defaultBranch {
		if _, err := runGit(ctx, sourceRepo, "merge", "--ff-only", remoteRef); err != nil {
			warns = append(warns, fmt.Sprintf("post-merge: fast-forward of %q failed (left as is): %v", defaultBranch, err))
		}
		return warns
	}

	localSHA, err := runGit(ctx, sourceRepo, "rev-parse", "--verify", "--quiet", localRef)
	if err != nil {
		return nil // no local default branch to update
	}
	localSHA = strings.TrimSpace(localSHA)
	if localSHA == remoteSHA {
		return nil
	}
	if _, err := runGit(ctx, sourceRepo, "merge-base", "--is-ancestor", localRef, remoteRef); err != nil {
		return []string{fmt.Sprintf("post-merge: local %q has diverged from origin/%s; left unchanged (host stays on %q)", defaultBranch, defaultBranch, cur)}
	}
	if wtPath, err := worktreeForBranch(ctx, sourceRepo, defaultBranch); err == nil && wtPath != "" {
		return []string{fmt.Sprintf("post-merge: %q is checked out in %s; not fast-forwarded (host stays on %q)", defaultBranch, wtPath, cur)}
	}
	if _, err := runGit(ctx, sourceRepo, "update-ref", localRef, remoteSHA, localSHA); err != nil {
		warns = append(warns, fmt.Sprintf("post-merge: could not fast-forward %q: %v", defaultBranch, err))
	}
	return warns
}

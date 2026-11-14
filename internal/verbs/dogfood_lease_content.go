package verbs

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// resolveClaimSHA pins a host-side commit before any lease mutation.
func resolveClaimSHA(ctx context.Context, sourceRepo, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	if strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("invalid sha %q", ref)
	}
	out, err := runGit(ctx, sourceRepo, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve requested sha %q: %w", ref, err)
	}
	return strings.TrimSpace(out), nil
}

// leaseContainment reads one branch snapshot lazily and caches ancestry by
// commit. It never takes the source lock while the lease lock is held.
func leaseContainment(ctx context.Context, lr *leaseRepo, warnings *[]string) func(string) (bool, error) {
	var head string
	var headErr error
	loaded := false
	type answer struct {
		ok  bool
		err error
	}
	cache := map[string]answer{}
	return func(sha string) (bool, error) {
		if a, ok := cache[sha]; ok {
			return a.ok, a.err
		}
		if !loaded {
			loaded = true
			out, err := runGit(ctx, lr.SourceRepo, "show-ref", "--verify", "--hash", "refs/heads/"+lr.Branch)
			if err != nil {
				// show-ref --verify exits 128 for a missing ref as well as Git failures.
				// Listing heads distinguishes an absent branch from an unreadable repo.
				refs, listErr := runGit(ctx, lr.SourceRepo, "for-each-ref", "--format=%(refname)", "refs/heads/"+lr.Branch)
				if listErr != nil || strings.Contains(refs, "refs/heads/"+lr.Branch+"\n") || strings.TrimSpace(refs) == "refs/heads/"+lr.Branch {
					headErr = err
				}
			} else {
				head = strings.TrimSpace(out)
			}
		}
		a := answer{err: headErr}
		if a.err == nil && head != "" {
			_, err := runGit(ctx, lr.SourceRepo, "merge-base", "--is-ancestor", sha, head)
			if err == nil {
				a.ok = true
			} else {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 1 {
					a.err = err
				}
			}
		}
		if a.err != nil {
			*warnings = append(*warnings, fmt.Sprintf("dogfood containment check for %s: %v", sha, a.err))
		}
		cache[sha] = a
		return a.ok, a.err
	}
}

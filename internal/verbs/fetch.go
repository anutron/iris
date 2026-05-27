package verbs

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// FetchInput captures the typed arguments for Fetch.
type FetchInput struct {
	Client *argus.Client
	TaskID string
}

// RefUpdate describes a single ref whose tracking SHA changed during a
// fetch. Empty OldSHA means the ref is newly created; empty NewSHA
// means the ref was pruned (only surfaced when iris adopts a prune
// flag — v1.1 does not, so this stays unused in practice).
type RefUpdate struct {
	Ref    string `json:"ref"`
	OldSHA string `json:"old_sha"`
	NewSHA string `json:"new_sha"`
}

// FetchResult is the structured success payload.
type FetchResult struct {
	Fetched     bool        `json:"fetched"`
	RefsUpdated []RefUpdate `json:"refs_updated"`
}

// Fetch runs `git fetch origin` in the resolved source repo under the
// per-source-repo lock, returning the list of refs whose tracking SHAs
// changed.
func Fetch(ctx context.Context, in FetchInput) (*FetchResult, error) {
	resolved, err := Resolve(ctx, in.Client, in.TaskID)
	if err != nil {
		return nil, err
	}

	mu := lockSourceRepo(resolved.SourceRepo)
	defer mu.Unlock()

	before, err := snapshotRemoteRefs(ctx, resolved.SourceRepo)
	if err != nil {
		return nil, fmt.Errorf("snapshot pre-fetch refs: %w", err)
	}

	if out, err := runGit(ctx, resolved.SourceRepo, "fetch", "origin"); err != nil {
		return nil, fmt.Errorf("fetch origin: %w; log:\n%s", err, out)
	}

	after, err := snapshotRemoteRefs(ctx, resolved.SourceRepo)
	if err != nil {
		return nil, fmt.Errorf("snapshot post-fetch refs: %w", err)
	}

	updates := diffRemoteRefs(before, after)
	return &FetchResult{
		Fetched:     true,
		RefsUpdated: updates,
	}, nil
}

// snapshotRemoteRefs returns a map of refs/remotes/origin/* -> SHA by
// reading `git for-each-ref refs/remotes/origin`. Used by Fetch to
// compute the pre/post diff.
func snapshotRemoteRefs(ctx context.Context, sourceRepo string) (map[string]string, error) {
	out, err := runGit(ctx, sourceRepo, "for-each-ref", "--format=%(refname) %(objectname)", "refs/remotes/origin")
	if err != nil {
		return nil, err
	}
	snap := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Skip the HEAD symbolic ref so its movement doesn't get reported
		// as a separate "ref updated" entry alongside the branch it tracks.
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if fields[0] == "refs/remotes/origin/HEAD" {
			continue
		}
		snap[fields[0]] = fields[1]
	}
	return snap, nil
}

// diffRemoteRefs returns the set of refs whose SHA changed between two
// snapshots (added refs surface as OldSHA="", removed as NewSHA="").
func diffRemoteRefs(before, after map[string]string) []RefUpdate {
	var updates []RefUpdate
	for ref, newSHA := range after {
		oldSHA := before[ref]
		if oldSHA != newSHA {
			updates = append(updates, RefUpdate{Ref: ref, OldSHA: oldSHA, NewSHA: newSHA})
		}
	}
	for ref, oldSHA := range before {
		if _, ok := after[ref]; !ok {
			updates = append(updates, RefUpdate{Ref: ref, OldSHA: oldSHA, NewSHA: ""})
		}
	}
	return updates
}

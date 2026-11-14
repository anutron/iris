package verbs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anutron/iris/internal/argus"
)

// DogfoodDeployOpts requests either a finished ref or a merge onto dogfood.
type DogfoodDeployOpts struct {
	Ref           string
	Compose       bool
	Force         bool
	LeaseOverride bool
	Caller        string
}

// DogfoodDeploy builds and verifies the selected tree without a checkout race.
// SetDogfood owns the shared lease gate and the full source-lock lifecycle.
func DogfoodDeploy(ctx context.Context, client *argus.Client, taskID string, opts DogfoodDeployOpts) (*SetDogfoodResult, error) {
	if strings.TrimSpace(opts.Ref) == "" {
		return nil, fmt.Errorf("ref is required")
	}
	return SetDogfood(ctx, client, taskID, SetDogfoodOpts{
		Sha: opts.Ref, compose: opts.Compose, Force: opts.Force, LeaseOverride: opts.LeaseOverride, Caller: opts.Caller,
		Manifest: &DogfoodManifest{Base: ManifestBase{Ref: opts.Ref}, Note: "iris_dogfood_deploy"},
	})
}

// composeDogfood uses a temporary branch so failed composition cannot move the
// dogfood tip. The caller holds the source lock until cleanup completes.
func composeDogfood(ctx context.Context, repo, base, ref string) (string, func(), error) {
	entry, err := currentBranch(ctx, repo)
	if err != nil {
		return "", nil, err
	}
	if entry == "" {
		return "", nil, fmt.Errorf("compose requires an attached host branch")
	}
	scratch := fmt.Sprintf("iris-deploy-%d", time.Now().UnixNano())
	if out, err := runDeploymentGit(ctx, repo, "checkout", "-b", scratch, base); err != nil {
		return "", nil, fmt.Errorf("compose checkout: %w; %s", err, out)
	}
	cleanup := func() {
		// On success SetDogfood already selected dogfood. On failure restore entry.
		branch, _ := currentBranch(ctx, repo)
		if branch == scratch {
			_, _ = runDeploymentGit(ctx, repo, "checkout", entry)
		}
		_, _ = runDeploymentGit(ctx, repo, "branch", "-D", scratch)
	}
	if out, err := runDeploymentGit(ctx, repo, "merge", "--no-ff", "--no-edit", ref); err != nil {
		abortOut, abortErr := runDeploymentGit(ctx, repo, "merge", "--abort")
		cleanup()
		if abortErr != nil {
			return "", nil, fmt.Errorf("compose failed: %w; %s; merge abort: %v; %s", err, out, abortErr, abortOut)
		}
		return "", nil, fmt.Errorf("compose failed (merge aborted; dogfood unchanged): %w; %s", err, out)
	}
	sha, err := runDeploymentGit(ctx, repo, "rev-parse", "HEAD")
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return strings.TrimSpace(sha), cleanup, nil
}

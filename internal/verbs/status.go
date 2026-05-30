package verbs

import (
	"context"
	"strings"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
)

// StatusInput is the public input shape for iris:status.
type StatusInput struct {
	TaskID string
	Path   string
}

// StatusResult is the structured result.
type StatusResult struct {
	SourceRepo       string            `json:"source_repo"`
	HeadSha          string            `json:"head_sha"`
	Branch           string            `json:"branch"`
	DefaultBranch    string            `json:"default_branch"`
	OriginDefaultSha string            `json:"origin_default_sha"`
	WorkingTreeClean bool              `json:"working_tree_clean"`
	Drift            bool              `json:"drift"`
	UpToDate         bool              `json:"up_to_date"`
	Config           *config.IrisToml  `json:"config"`
	// ConfigSources maps each top-level TOML field name that was set in at
	// least one of `.iris.toml` / `.iris.local.toml` to its source — either
	// "shared" or "local". Fields unset in both files are omitted (no "none"
	// sentinel). When neither file exists, the field is an empty object
	// `{}` rather than nil/absent so consumers can rely on the shape.
	ConfigSources    map[string]string `json:"config_sources"`
	ArgusTask        *argus.Task       `json:"argus_task"`
	LastReload       *AuditEntry       `json:"last_reload"`
	Dogfood          *DogfoodManifest  `json:"dogfood"`
	Warnings         []string          `json:"warnings"`
}

// Status resolves the target, reads .iris.toml non-fatally, captures git
// state, reads the most recent audit entry, and returns one structured
// object. No side effects.
func Status(ctx context.Context, client *argus.Client, in StatusInput) (*StatusResult, error) {
	target, err := ResolveTarget(ctx, client, in.TaskID, in.Path)
	if err != nil {
		return nil, err
	}

	warnings := []string{}

	// .iris.toml + optional .iris.local.toml. A missing shared file is a
	// non-event (silent null config). Parse errors and cross-validation
	// errors still surface as warnings. The overlay loader tracks which
	// file each top-level field came from so we can populate config_sources.
	isSelf := false
	if self, err := ResolveSelf(ctx); err == nil {
		isSelf = EqualSourceRepos(target.SourceRepo, self.SourceRepo)
	}
	overlay, _ := config.LoadOverlay(target.SourceRepo, isSelf)
	// configSources must be non-nil so it JSON-marshals as `{}` (not `null`)
	// when no files exist — consumers rely on the field always being an
	// object.
	configSources := map[string]string{}
	var verrs []config.ValidationError
	var cfg *config.IrisToml
	if overlay != nil {
		verrs = append(verrs, overlay.ValidationErrors...)
		if overlay.Doc != nil {
			// Layer cross-validation on top of overlay parse errors so
			// schema violations continue to surface in Status (matching
			// the previous LoadIrisToml-based behavior).
			verrs = append(verrs, overlay.Doc.Validate(isSelf)...)
		}
		for k, v := range overlay.Provenance {
			configSources[k] = string(v)
		}
		if len(verrs) == 0 && overlay.Doc != nil {
			cfg = overlay.Doc
		}
	}
	for _, e := range verrs {
		warnings = append(warnings, e.Error())
	}

	// Default branch.
	defaultBranch := ""
	if cfg != nil && cfg.DefaultBranch != "" {
		defaultBranch = cfg.DefaultBranch
	} else if db, err := DefaultBranch(ctx, target.SourceRepo); err == nil {
		defaultBranch = db
	} else {
		defaultBranch = "main"
		warnings = append(warnings, "origin/HEAD unset, defaulted to main")
	}

	// HEAD sha.
	head, _ := runGit(ctx, target.SourceRepo, "rev-parse", "HEAD")
	headSha := strings.TrimSpace(head)

	// Current branch. `git rev-parse --abbrev-ref HEAD` returns the literal
	// "HEAD" when detached; normalize that to an empty string so consumers
	// can distinguish "on a branch" from "detached" without parsing.
	branch := ""
	if b, err := currentBranch(ctx, target.SourceRepo); err == nil {
		if b != "HEAD" {
			branch = b
		}
	}

	// Reverse-lookup an argus task that owns this source repo. Iris does
	// not fail Status when argus is unreachable: surface a warning and
	// leave argus_task null.
	var argusTask *argus.Task
	if client != nil {
		t, err := FindTaskBySourceRepo(ctx, client, target.SourceRepo)
		if err != nil {
			warnings = append(warnings, "could not query argus for matching task: "+err.Error())
		} else {
			argusTask = t
		}
	}

	// origin/<default>'s SHA — fetch is not allowed (no side effects), so
	// rely on what's already in the local objects.
	origin, _ := runGit(ctx, target.SourceRepo, "rev-parse", "origin/"+defaultBranch)
	originSha := strings.TrimSpace(origin)

	// Working tree clean?
	status, _ := runGit(ctx, target.SourceRepo, "status", "--porcelain=v1")
	clean := strings.TrimSpace(status) == ""

	// Last reload from audit log.
	last, _ := LatestForRepo(target.SourceRepo)

	drift := false
	if last != nil && last.PostPullSha != "" && headSha != "" && last.PostPullSha != headSha {
		drift = true
	}
	upToDate := false
	if originSha != "" && headSha == originSha {
		upToDate = true
	}

	if last == nil {
		warnings = append(warnings, "no reload recorded for this system yet")
	}

	var dogfood *DogfoodManifest
	if stateDir, err := SourceRepoStateDir(target.SourceRepo); err == nil {
		manifest, merr := ReadManifest(stateDir)
		if merr != nil {
			warnings = append(warnings, merr.Error())
		} else if manifest != nil {
			dogfood = manifest
		}
	}

	return &StatusResult{
		SourceRepo:       target.SourceRepo,
		HeadSha:          headSha,
		Branch:           branch,
		DefaultBranch:    defaultBranch,
		OriginDefaultSha: originSha,
		WorkingTreeClean: clean,
		Drift:            drift,
		UpToDate:         upToDate,
		Config:           cfg,
		ConfigSources:    configSources,
		ArgusTask:        argusTask,
		LastReload:       last,
		Dogfood:          dogfood,
		Warnings:         warnings,
	}, nil
}

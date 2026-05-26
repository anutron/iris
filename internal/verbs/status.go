package verbs

import (
	"context"
	"path/filepath"
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
	SourceRepo        string           `json:"source_repo"`
	HeadSha           string           `json:"head_sha"`
	DefaultBranch     string           `json:"default_branch"`
	OriginDefaultSha  string           `json:"origin_default_sha"`
	WorkingTreeClean  bool             `json:"working_tree_clean"`
	Drift             bool             `json:"drift"`
	UpToDate          bool             `json:"up_to_date"`
	Config            *config.IrisToml `json:"config"`
	LastReload        *AuditEntry      `json:"last_reload"`
	Warnings          []string         `json:"warnings"`
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

	// .iris.toml (non-fatal: missing or malformed surface as warnings).
	isSelf := false
	if self, err := ResolveSelf(ctx); err == nil {
		isSelf = EqualSourceRepos(target.SourceRepo, self.SourceRepo)
	}
	tomlPath := filepath.Join(target.SourceRepo, config.IrisTomlFilename)
	doc, verrs, _ := config.LoadIrisToml(tomlPath, isSelf)
	if len(verrs) > 0 {
		for _, e := range verrs {
			warnings = append(warnings, e.Error())
		}
	}
	var cfg *config.IrisToml
	if len(verrs) == 0 && doc != nil {
		cfg = doc
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

	return &StatusResult{
		SourceRepo:       target.SourceRepo,
		HeadSha:          headSha,
		DefaultBranch:    defaultBranch,
		OriginDefaultSha: originSha,
		WorkingTreeClean: clean,
		Drift:            drift,
		UpToDate:         upToDate,
		Config:           cfg,
		LastReload:       last,
		Warnings:         warnings,
	}, nil
}

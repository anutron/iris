package verbs

import (
	"context"
	"fmt"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
)

// ValidateConfigInput is the public input shape for iris:validate_config.
type ValidateConfigInput struct {
	TaskID string
	Path   string
}

// ValidateConfigResult is the structured result.
//
// Warnings are structured (OverlayWarning) so consumers can distinguish a
// taxonomy violation in the shared file from one in the local file, and so
// the migration hint travels as a separate field rather than being baked
// into a single message string. Warnings are non-fatal: their presence does
// NOT flip `valid` to false. Only ValidationErrors do.
type ValidateConfigResult struct {
	Valid    bool                     `json:"valid"`
	Errors   []config.ValidationError `json:"errors"`
	Warnings []config.OverlayWarning  `json:"warnings"`
	Resolved *config.IrisToml         `json:"resolved,omitempty"`
}

// ValidateConfig parses the `.iris.toml` and optional `.iris.local.toml` at
// the resolved source repo, merges them via the overlay loader, and
// cross-validates the merged document without any side effects (no pull,
// no build, no restart, no audit-log write).
//
// Warnings vs errors:
//   - Taxonomy violations (a local-tagged field in `.iris.toml`, or a
//     shared-tagged field in `.iris.local.toml`) flow through as
//     OverlayWarnings. They do NOT flip valid to false — graceful migration
//     per design.md.
//   - Parse failures (malformed TOML in either file), unknown fields, and
//     cross-validation errors flow through as ValidationErrors and DO flip
//     valid to false. A malformed `.iris.local.toml` produces a structured
//     error naming the local file, and `.iris.toml`'s fields survive on
//     the resolved doc (the overlay loader's contract).
func ValidateConfig(ctx context.Context, client *argus.Client, in ValidateConfigInput) (*ValidateConfigResult, error) {
	target, err := ResolveTarget(ctx, client, in.TaskID, in.Path)
	if err != nil {
		return nil, err
	}

	isSelf := false
	if self, err := ResolveSelf(ctx); err == nil {
		isSelf = EqualSourceRepos(target.SourceRepo, self.SourceRepo)
	}

	overlay, err := config.LoadOverlay(target.SourceRepo, isSelf)
	if err != nil {
		return nil, err
	}

	verrs := overlay.ValidationErrors
	// validate_config requires a config to validate. LoadOverlay is silent
	// on missing files (callers that treat the config as optional rely on
	// that), so this verb synthesizes the file-not-found error itself when
	// the shared `.iris.toml` is absent.
	if overlay.Doc == nil && len(verrs) == 0 {
		verrs = append(verrs, config.ValidationError{
			Field:   config.IrisTomlFilename,
			Message: fmt.Sprintf("file not found at %s/%s", target.SourceRepo, config.IrisTomlFilename),
			Hint:    "create .iris.toml at the source repo root",
		})
	}

	// Cross-validate the merged document. LoadOverlay does NOT run
	// IrisToml.Validate (it sticks to parse-level errors and taxonomy
	// warnings), so the verb layers cross-validation on top. Skipped when
	// the doc is nil (parse failed or shared file is missing).
	if overlay.Doc != nil {
		verrs = append(verrs, overlay.Doc.Validate(isSelf)...)
	}

	result := &ValidateConfigResult{
		Valid:    len(verrs) == 0,
		Errors:   verrs,
		Warnings: overlay.Warnings,
	}
	if result.Valid && overlay.Doc != nil {
		result.Resolved = overlay.Doc
	}
	if result.Errors == nil {
		result.Errors = []config.ValidationError{}
	}
	if result.Warnings == nil {
		result.Warnings = []config.OverlayWarning{}
	}
	return result, nil
}

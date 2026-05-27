package verbs

import (
	"context"
	"path/filepath"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
)

// ValidateConfigInput is the public input shape for iris:validate_config.
type ValidateConfigInput struct {
	TaskID string
	Path   string
}

// ValidateConfigResult is the structured result.
type ValidateConfigResult struct {
	Valid    bool                      `json:"valid"`
	Errors   []config.ValidationError  `json:"errors"`
	Warnings []string                  `json:"warnings"`
	Resolved *config.IrisToml          `json:"resolved,omitempty"`
}

// ValidateConfig parses the `.iris.toml` at the resolved source repo and
// cross-validates it without any side effects (no pull, no build, no
// restart, no audit-log write).
func ValidateConfig(ctx context.Context, client *argus.Client, in ValidateConfigInput) (*ValidateConfigResult, error) {
	target, err := ResolveTarget(ctx, client, in.TaskID, in.Path)
	if err != nil {
		return nil, err
	}

	isSelf := false
	if self, err := ResolveSelf(ctx); err == nil {
		isSelf = EqualSourceRepos(target.SourceRepo, self.SourceRepo)
	}

	tomlPath := filepath.Join(target.SourceRepo, config.IrisTomlFilename)
	doc, verrs, err := config.LoadIrisToml(tomlPath, isSelf)
	if err != nil {
		return nil, err
	}

	result := &ValidateConfigResult{
		Valid:    len(verrs) == 0,
		Errors:   verrs,
		Warnings: []string{},
	}
	if result.Valid && doc != nil {
		result.Resolved = doc
	}
	if result.Errors == nil {
		result.Errors = []config.ValidationError{}
	}
	return result, nil
}

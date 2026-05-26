package verbs

import (
	"context"
	"fmt"
	"strings"

	"github.com/anutron/iris/internal/argus"
)

// DefaultTagMessage is the annotation iris uses when the caller does
// not supply one.
const DefaultTagMessage = "Released by iris"

// TagInput captures the typed arguments for Tag.
type TagInput struct {
	Client  *argus.Client
	TaskID  string
	Tag     string
	Message string
}

// TagResult is the structured success payload.
type TagResult struct {
	Tagged  bool   `json:"tagged"`
	Tag     string `json:"tag"`
	SHA     string `json:"sha"`
	Message string `json:"message"`
}

// Tag creates an annotated tag at the SHA of origin/<default-branch>
// and pushes it to origin under the per-source-repo lock. Refuses if
// the tag already exists locally or on origin.
func Tag(ctx context.Context, in TagInput) (*TagResult, error) {
	if in.Tag == "" {
		return nil, fmt.Errorf("tag is required")
	}
	// argv flag-smuggling guard: refuse tag names that start with `-` so
	// they can't be interpreted as a git option (e.g. `--exec=evil`).
	// Real tag names never begin with `-`.
	if strings.HasPrefix(in.Tag, "-") {
		return nil, fmt.Errorf("invalid tag name %q (must not begin with '-')", in.Tag)
	}
	message := in.Message
	if message == "" {
		message = DefaultTagMessage
	}

	resolved, err := Resolve(ctx, in.Client, in.TaskID)
	if err != nil {
		return nil, err
	}

	defaultBranch, err := DefaultBranch(ctx, resolved.SourceRepo)
	if err != nil {
		return nil, fmt.Errorf("determine default branch: %w", err)
	}

	mu := lockSourceRepo(resolved.SourceRepo)
	defer mu.Unlock()

	if sha, err := runGit(ctx, resolved.SourceRepo, "rev-parse", "--verify", "refs/tags/"+in.Tag); err == nil && strings.TrimSpace(sha) != "" {
		return nil, fmt.Errorf("tag %q already exists locally at %s", in.Tag, strings.TrimSpace(sha))
	}
	if remoteSHA, err := remoteTagSHA(ctx, resolved.SourceRepo, in.Tag); err != nil {
		return nil, err
	} else if remoteSHA != "" {
		return nil, fmt.Errorf("tag %q already exists on origin at %s", in.Tag, remoteSHA)
	}

	target := "origin/" + defaultBranch
	targetSHA, err := runGit(ctx, resolved.SourceRepo, "rev-parse", target)
	if err != nil {
		return nil, fmt.Errorf("rev-parse %s: %w", target, err)
	}
	sha := strings.TrimSpace(targetSHA)

	if out, err := runGit(ctx, resolved.SourceRepo, "tag", "-a", in.Tag, "-m", message, sha); err != nil {
		return nil, fmt.Errorf("tag -a %s: %w; log:\n%s", in.Tag, err, out)
	}

	if out, err := runGit(ctx, resolved.SourceRepo, "push", "origin", in.Tag); err != nil {
		return nil, fmt.Errorf("push origin %s: %w; log:\n%s", in.Tag, err, out)
	}

	return &TagResult{
		Tagged:  true,
		Tag:     in.Tag,
		SHA:     sha,
		Message: message,
	}, nil
}

// remoteTagSHA returns origin's SHA for the named tag via
// `git ls-remote --tags origin <tag>`, or "" when origin does not
// have it.
func remoteTagSHA(ctx context.Context, sourceRepo, tag string) (string, error) {
	out, err := runGit(ctx, sourceRepo, "ls-remote", "--tags", "origin", tag)
	if err != nil {
		return "", fmt.Errorf("ls-remote --tags origin %s: %w", tag, err)
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return "", nil
	}
	fields := strings.Fields(trimmed)
	if len(fields) < 1 {
		return "", nil
	}
	return fields[0], nil
}

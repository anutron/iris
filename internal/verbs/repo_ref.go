package verbs

import (
	"context"
	"regexp"
	"strings"
	"sync"
)

// RepoRef names the source repo a verb resolved: the GitHub `owner/name`
// parsed from its origin remote (empty when unparseable) and its canonical
// local path. The MCP server attaches it to every response so a caller
// passing another project's task_id can see which repo was acted on.
type RepoRef struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path"`
}

// String renders the ref as "owner/name (path)", or just the path when
// there is no name.
func (r RepoRef) String() string {
	if r.Name == "" {
		return r.Path
	}
	return r.Name + " (" + r.Path + ")"
}

// RepoRecorder captures the first repo resolved during one request. It is
// safe for concurrent use: worktree runs record from a goroutine while the
// handler may read.
type RepoRecorder struct {
	mu  sync.Mutex
	ref *RepoRef
}

type repoRecorderKey struct{}

// WithRepoRecorder returns a child context carrying a fresh recorder.
func WithRepoRecorder(ctx context.Context) (context.Context, *RepoRecorder) {
	rec := &RepoRecorder{}
	return context.WithValue(ctx, repoRecorderKey{}, rec), rec
}

// Ref returns the recorded repo, or nil when nothing was resolved.
func (r *RepoRecorder) Ref() *RepoRef {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ref == nil {
		return nil
	}
	ref := *r.ref
	return &ref
}

// Record stores ref unless a repo was already recorded (first wins).
func (r *RepoRecorder) Record(ref RepoRef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ref == nil {
		r.ref = &ref
	}
}

func (r *RepoRecorder) empty() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ref == nil
}

func recorderFrom(ctx context.Context) *RepoRecorder {
	rec, _ := ctx.Value(repoRecorderKey{}).(*RepoRecorder)
	return rec
}

// RecordRepo records ref into ctx's recorder, if ctx carries one.
func RecordRepo(ctx context.Context, ref *RepoRef) {
	if ref == nil {
		return
	}
	if rec := recorderFrom(ctx); rec != nil {
		rec.Record(*ref)
	}
}

// recordResolved records sourceRepo into ctx's recorder. The origin lookup
// only runs when a recorder is present and still empty, so CLI and direct
// verb calls pay nothing.
func recordResolved(ctx context.Context, sourceRepo string) {
	rec := recorderFrom(ctx)
	if rec == nil || !rec.empty() {
		return
	}
	ref := RepoRef{Path: sourceRepo}
	if out, err := runGit(ctx, sourceRepo, "remote", "get-url", "origin"); err == nil {
		ref.Name = ParseGitHubRepoName(strings.TrimSpace(out))
	}
	rec.Record(ref)
}

// githubRepoRegexp matches the owner/name tail of GitHub-style remote URLs:
// git@host:owner/name(.git), ssh://git@host/owner/name(.git), and
// https://host/owner/name(.git)(/).
var githubRepoRegexp = regexp.MustCompile(`^(?:[a-z][a-z0-9+.-]*://(?:[^@/]+@)?[^/]+/|[^@/:]+@[^:/]+:)([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?/?$`)

// ParseGitHubRepoName extracts "owner/name" from a remote URL, or returns
// "" for anything that isn't a host-qualified owner/name remote (local
// paths, file:// URLs, deeper paths).
func ParseGitHubRepoName(url string) string {
	if strings.HasPrefix(url, "file://") {
		return ""
	}
	m := githubRepoRegexp.FindStringSubmatch(url)
	if m == nil {
		return ""
	}
	return m[1] + "/" + m[2]
}

// prURLRegexp extracts owner/name from a GitHub PR URL.
var prURLRegexp = regexp.MustCompile(`^https?://[^/]+/([^/]+)/([^/]+)/pull/\d+`)

// repoFromPRURL returns "owner/name" for a PR URL, or "".
func repoFromPRURL(url string) string {
	m := prURLRegexp.FindStringSubmatch(strings.TrimSpace(url))
	if m == nil {
		return ""
	}
	return m[1] + "/" + m[2]
}

// expectedRepoRegexp is the accepted shape of a caller's expected_repo.
var expectedRepoRegexp = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// ValidExpectedRepo reports whether s has the form owner/name.
func ValidExpectedRepo(s string) bool {
	return expectedRepoRegexp.MatchString(s)
}

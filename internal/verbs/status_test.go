package verbs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anutron/iris/internal/argus"
)

// statusFixture returns a source repo (committed clean, origin/HEAD set,
// .iris.toml present) plus a stub argus that allowlists it.
func statusFixture(t *testing.T, body string) (string, *argus.Client) {
	t.Helper()
	setAuditDir(t)
	src := setupRepoOnly(t)
	if body != "" {
		if err := os.WriteFile(filepath.Join(src, ".iris.toml"), []byte(body), 0o644); err != nil {
			t.Fatalf("write toml: %v", err)
		}
		g := gitRunner(t)
		g(src, "add", ".iris.toml")
		g(src, "commit", "-m", "fixture toml")
		g(src, "push", "origin", "main")
	}
	canon, _ := filepath.EvalSymlinks(src)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/projects/full" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"name": "iris-test", "path": canon}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	// Make ResolveSelf point elsewhere so this is a cross-target.
	elsewhere := setupRepoOnly(t)
	bin := filepath.Join(elsewhere, "bin", "iris")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	_ = os.WriteFile(bin, []byte("x"), 0o755)
	old := executable
	executable = func() (string, error) { return bin, nil }
	t.Cleanup(func() { executable = old })
	return src, argus.New(srv.URL, "stub-token")
}

const tomlNone = `schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "none"
`

func TestStatus_HappyPath(t *testing.T) {
	src, client := statusFixture(t, tomlNone)
	canon, _ := filepath.EvalSymlinks(src)

	// Seed an audit entry that matches the current head (no drift).
	headBytes, _ := readGitOutput(src, "rev-parse", "HEAD")
	head := strings.TrimSpace(headBytes)
	_ = AppendAudit(AuditEntry{
		Timestamp:        time.Now().UTC().Add(-1 * time.Hour),
		TargetSourceRepo: canon,
		Outcome:          "success",
		PostPullSha:      head,
	})

	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.SourceRepo != canon {
		t.Fatalf("source_repo = %q want %q", res.SourceRepo, canon)
	}
	if res.HeadSha == "" {
		t.Fatal("empty head_sha")
	}
	if res.DefaultBranch != "main" {
		t.Fatalf("default_branch = %q", res.DefaultBranch)
	}
	if !res.WorkingTreeClean {
		t.Fatal("expected working tree clean")
	}
	if res.Drift {
		t.Fatal("expected drift=false when post_pull_sha == head")
	}
	if !res.UpToDate {
		t.Fatal("expected up_to_date=true when head == origin/main")
	}
	if res.Config == nil || res.Config.Restart.Mechanism != "none" {
		t.Fatalf("expected config parsed, got: %+v", res.Config)
	}
	if res.LastReload == nil {
		t.Fatal("expected last_reload to be populated")
	}
}

func readGitOutput(dir string, args ...string) (string, error) {
	return runGit(context.Background(), dir, args...)
}

func TestStatus_DriftWhenHeadDiffersFromLastReload(t *testing.T) {
	src, client := statusFixture(t, tomlNone)
	canon, _ := filepath.EvalSymlinks(src)
	_ = AppendAudit(AuditEntry{
		Timestamp:        time.Now().UTC().Add(-1 * time.Hour),
		TargetSourceRepo: canon,
		Outcome:          "success",
		PostPullSha:      "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
	})
	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !res.Drift {
		t.Fatal("expected drift=true when head differs from post_pull_sha")
	}
}

func TestStatus_NotUpToDateWhenOriginAhead(t *testing.T) {
	src, client := statusFixture(t, tomlNone)
	// Advance origin via another clone and a push.
	g := gitRunner(t)
	other := t.TempDir()
	g("", "clone", filepath.Join(filepath.Dir(src), "origin.git"), other)
	g(other, "config", "user.email", "x@y.z")
	g(other, "config", "user.name", "x")
	g(other, "commit", "--allow-empty", "-m", "origin-ahead")
	g(other, "push", "origin", "main")
	// Now fetch (without merge) into the source repo so origin/main local
	// ref advances. We're allowed to fetch because Status itself does not
	// fetch — this is test setup.
	g(src, "fetch", "origin", "main")

	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.UpToDate {
		t.Fatal("expected up_to_date=false when origin is ahead")
	}
}

func TestStatus_MissingAuditLogReturnsLastReloadNil(t *testing.T) {
	src, client := statusFixture(t, tomlNone)
	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.LastReload != nil {
		t.Fatalf("expected last_reload nil, got: %+v", res.LastReload)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "no reload recorded") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected warning about no reload, got: %+v", res.Warnings)
	}
}

func TestStatus_MissingIrisTomlSurfacedAsWarning(t *testing.T) {
	src, client := statusFixture(t, "") // no .iris.toml
	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.Config != nil {
		t.Fatal("expected config nil when .iris.toml missing")
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, ".iris.toml") || strings.Contains(w, "file not found") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected .iris.toml warning, got: %+v", res.Warnings)
	}
}

func TestStatus_NoSideEffects(t *testing.T) {
	src, client := statusFixture(t, tomlNone)
	dir := os.Getenv(AuditDirEnv)
	beforeFiles, _ := readNames(dir)
	beforeHead := headSHA(t, src)
	if _, err := Status(context.Background(), client, StatusInput{Path: src}); err != nil {
		t.Fatalf("status: %v", err)
	}
	if h := headSHA(t, src); h != beforeHead {
		t.Fatalf("expected no git mutation: before=%s after=%s", beforeHead, h)
	}
	afterFiles, _ := readNames(dir)
	if len(beforeFiles) != len(afterFiles) {
		t.Fatalf("expected no audit-dir mutation: before=%v after=%v", beforeFiles, afterFiles)
	}
}

func TestStatus_SelfTargetWhenNoInputs(t *testing.T) {
	// Build a fixture and point executable into it so ResolveSelf resolves
	// to this repo. Status() with no inputs should target it.
	src := setupRepoOnly(t)
	if err := os.WriteFile(filepath.Join(src, ".iris.toml"), []byte(`schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "exit_code"
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	bin := filepath.Join(src, "bin", "iris")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	_ = os.WriteFile(bin, []byte("x"), 0o755)
	old := executable
	executable = func() (string, error) { return bin, nil }
	t.Cleanup(func() { executable = old })

	res, err := Status(context.Background(), nil, StatusInput{})
	if err != nil {
		t.Fatalf("status self: %v", err)
	}
	canon, _ := filepath.EvalSymlinks(src)
	if res.SourceRepo != canon {
		t.Fatalf("expected self src %s, got %s", canon, res.SourceRepo)
	}
}

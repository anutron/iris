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
// .iris.toml present when body != "") plus a stub argus that allowlists
// it. The stub answers /api/tasks with an empty list by default;
// statusFixtureWithTasks supports seeding tasks.
func statusFixture(t *testing.T, body string) (string, *argus.Client) {
	return statusFixtureWithTasks(t, body, nil)
}

// statusFixtureWithTasks is the configurable variant. tasks (when
// non-nil) is returned from /api/tasks so FindTaskBySourceRepo has a
// chance to match.
func statusFixtureWithTasks(t *testing.T, body string, tasks []map[string]any) (string, *argus.Client) {
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
	if tasks == nil {
		tasks = []map[string]any{}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/projects/full":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"name": "iris-test", "path": canon}},
			})
		case "/api/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks})
		default:
			http.NotFound(w, r)
		}
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

// TestStatus_MissingIrisTomlIsSilent verifies the consumer-ergonomics
// contract: a missing `.iris.toml` is NOT a warning; it produces
// config: nil with no warning emitted about the missing file.
func TestStatus_MissingIrisTomlIsSilent(t *testing.T) {
	src, client := statusFixture(t, "") // no .iris.toml
	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.Config != nil {
		t.Fatal("expected config nil when .iris.toml missing")
	}
	for _, w := range res.Warnings {
		if strings.Contains(w, ".iris.toml") || strings.Contains(w, "file not found") {
			t.Fatalf("expected no warning about missing .iris.toml, got: %q", w)
		}
	}
}

// TestStatus_MalformedIrisTomlStillWarns verifies that parse errors
// continue to surface as warnings (the silent-missing contract only
// applies to ENOENT, not to malformed content).
func TestStatus_MalformedIrisTomlStillWarns(t *testing.T) {
	src, client := statusFixture(t, "")
	// Write malformed TOML directly (bypass fixture's commit, so we don't
	// reject schema_version validation – we want a parse error specifically).
	if err := os.WriteFile(filepath.Join(src, ".iris.toml"), []byte("broken = [\n"), 0o644); err != nil {
		t.Fatalf("write malformed: %v", err)
	}
	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.Config != nil {
		t.Fatal("expected config nil when .iris.toml malformed")
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "parse error") || strings.Contains(w, "TOML") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected parse-error warning, got: %+v", res.Warnings)
	}
}

// TestStatus_BranchAndArgusTask verifies the new fields populate when
// the source repo's HEAD is on a branch and argus advertises a task
// whose worktree_path equals the canonicalized source repo.
func TestStatus_BranchAndArgusTask(t *testing.T) {
	tasks := []map[string]any{
		{"id": "other", "worktree_path": "/some/elsewhere"},
		// Filled in after we know canon path below.
	}
	src, client := statusFixtureWithTasks(t, tomlNone, tasks)
	canon, _ := filepath.EvalSymlinks(src)
	// Add a feature branch and switch to it.
	g := gitRunner(t)
	g(src, "checkout", "-b", "argus/feature-x")
	// Re-spin the fixture with the canonical path in a task. (Simplest:
	// rebuild the client with a fresh fixture.) Instead, run another
	// fixture iteration: but easier — make a second fixture pointing at
	// the same src is not possible because setupRepoOnly creates a fresh
	// repo. So just construct the argus stub directly here.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/projects/full":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"name": "iris-test", "path": canon}},
			})
		case "/api/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tasks": []map[string]any{
					{"id": "other", "worktree_path": "/some/elsewhere"},
					{"id": "match", "name": "matched task", "project": "iris",
						"status": "in_progress", "worktree_path": canon,
						"branch": "argus/feature-x"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client = argus.New(srv.URL, "stub")

	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.Branch != "argus/feature-x" {
		t.Fatalf("branch = %q want %q", res.Branch, "argus/feature-x")
	}
	if res.ArgusTask == nil {
		t.Fatal("expected argus_task to be populated")
	}
	if res.ArgusTask.ID != "match" {
		t.Fatalf("argus_task.id = %q want match", res.ArgusTask.ID)
	}
}

// TestStatus_ArgusTaskNullWhenNoMatch confirms the absence path is
// silent (null task, no warning) – this is the "argus has tasks but
// none point at this source repo" case.
func TestStatus_ArgusTaskNullWhenNoMatch(t *testing.T) {
	src, client := statusFixtureWithTasks(t, tomlNone, []map[string]any{
		{"id": "elsewhere", "worktree_path": "/some/elsewhere"},
	})
	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.ArgusTask != nil {
		t.Fatalf("expected argus_task nil, got %#v", res.ArgusTask)
	}
	for _, w := range res.Warnings {
		if strings.Contains(w, "argus") {
			t.Fatalf("expected no argus warning when list-tasks succeeded, got: %q", w)
		}
	}
}

// TestStatus_ArgusUnreachableSurfacesWarning confirms that when argus
// errors on /api/tasks (server 500), Status still succeeds, argus_task
// is nil, and a warning is appended.
func TestStatus_ArgusUnreachableSurfacesWarning(t *testing.T) {
	// Build a fixture, then swap the argus client to one that 500s on
	// /api/tasks (but still allowlists / on projects so Resolve succeeds).
	src := setupRepoOnly(t)
	setAuditDir(t)
	canon, _ := filepath.EvalSymlinks(src)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/projects/full":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"name": "iris-test", "path": canon}},
			})
		case "/api/tasks":
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	elsewhere := setupRepoOnly(t)
	bin := filepath.Join(elsewhere, "bin", "iris")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	_ = os.WriteFile(bin, []byte("x"), 0o755)
	old := executable
	executable = func() (string, error) { return bin, nil }
	t.Cleanup(func() { executable = old })
	client := argus.New(srv.URL, "stub")

	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.ArgusTask != nil {
		t.Fatal("expected argus_task nil when argus errors")
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "could not query argus") || strings.Contains(w, "matching task") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an argus-unreachable warning, got: %+v", res.Warnings)
	}
}

// TestStatus_BranchEmptyOnDetachedHead verifies the detached-HEAD
// normalization: `git rev-parse --abbrev-ref HEAD` returns "HEAD" in
// that state; Status reports branch as "".
func TestStatus_BranchEmptyOnDetachedHead(t *testing.T) {
	src, client := statusFixture(t, tomlNone)
	g := gitRunner(t)
	// Detach HEAD by checking out the current commit by SHA.
	sha := strings.TrimSpace(headSHA(t, src))
	g(src, "checkout", "--detach", sha)

	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.Branch != "" {
		t.Fatalf("expected branch=\"\" on detached HEAD, got %q", res.Branch)
	}
}

// TestStatus_DogfoodManifestPresent verifies a valid manifest surfaces in
// the Dogfood field, round-tripped with RecordedAt populated, no warning.
func TestStatus_DogfoodManifestPresent(t *testing.T) {
	src, client := statusFixture(t, tomlNone)
	canon, err := filepath.EvalSymlinks(src)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	dir, err := SourceRepoStateDir(canon)
	if err != nil {
		t.Fatalf("SourceRepoStateDir: %v", err)
	}
	in := sampleManifest()
	if err := WriteManifest(dir, in); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.Dogfood == nil {
		t.Fatal("expected Dogfood populated when a valid manifest exists")
	}
	if res.Dogfood.Base != in.Base {
		t.Errorf("base mismatch: got %+v want %+v", res.Dogfood.Base, in.Base)
	}
	if len(res.Dogfood.Layered) != len(in.Layered) {
		t.Fatalf("layered len: got %d want %d", len(res.Dogfood.Layered), len(in.Layered))
	}
	for i := range in.Layered {
		if res.Dogfood.Layered[i] != in.Layered[i] {
			t.Errorf("layered[%d] mismatch: got %+v want %+v", i, res.Dogfood.Layered[i], in.Layered[i])
		}
	}
	if res.Dogfood.Note != in.Note {
		t.Errorf("note mismatch: got %q want %q", res.Dogfood.Note, in.Note)
	}
	if res.Dogfood.RecordedAt == "" {
		t.Error("expected RecordedAt populated (stamped by WriteManifest)")
	}
	for _, w := range res.Warnings {
		if strings.Contains(w, "manifest") {
			t.Fatalf("expected no manifest warning on a valid manifest, got: %q", w)
		}
	}
}

// TestStatus_DogfoodManifestAbsent verifies the absence path is silent:
// Dogfood nil, no manifest warning.
func TestStatus_DogfoodManifestAbsent(t *testing.T) {
	src, client := statusFixture(t, tomlNone) // fixture writes no manifest
	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.Dogfood != nil {
		t.Fatalf("expected Dogfood nil when no manifest exists, got: %+v", res.Dogfood)
	}
	for _, w := range res.Warnings {
		if strings.Contains(w, "manifest") {
			t.Fatalf("expected no manifest warning when manifest absent, got: %q", w)
		}
	}
}

// TestStatus_DogfoodManifestMalformedWarns verifies a malformed manifest
// yields Dogfood nil plus exactly one warning naming the manifest path and a
// parse error — Status never fails on it.
func TestStatus_DogfoodManifestMalformedWarns(t *testing.T) {
	src, client := statusFixture(t, tomlNone)
	canon, err := filepath.EvalSymlinks(src)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	dir, err := SourceRepoStateDir(canon)
	if err != nil {
		t.Fatalf("SourceRepoStateDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	mpath := filepath.Join(dir, DogfoodManifestFilename)
	if err := os.WriteFile(mpath, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("seed garbage manifest: %v", err)
	}

	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.Dogfood != nil {
		t.Fatalf("expected Dogfood nil on malformed manifest, got: %+v", res.Dogfood)
	}
	count := 0
	for _, w := range res.Warnings {
		if strings.Contains(w, DogfoodManifestFilename) {
			count++
			if !strings.Contains(w, "parse") {
				t.Errorf("manifest warning should name a parse error, got: %q", w)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one manifest warning naming %s, got %d: %+v",
			DogfoodManifestFilename, count, res.Warnings)
	}
}

// TestStatus_DogfoodManifestPreviousManifestSurvivesRead verifies the
// "previous_manifest survives manifest read" scenario from the spec: when an
// on-disk manifest has a previous_manifest, iris:status returns the full
// manifest including the embedded prior.
func TestStatus_DogfoodManifestPreviousManifestSurvivesRead(t *testing.T) {
	src, client := statusFixture(t, tomlNone)
	canon, err := filepath.EvalSymlinks(src)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	dir, err := SourceRepoStateDir(canon)
	if err != nil {
		t.Fatalf("SourceRepoStateDir: %v", err)
	}

	// First manifest establishes a prior; second manifest's WriteManifest
	// embeds it under previous_manifest.
	a := &DogfoodManifest{
		Base:    ManifestBase{Ref: "main", SHA: "aaa111"},
		Layered: []LayeredEntry{{Name: "F1", SHA: "fff111", Applied: "cherry-pick"}},
		Note:    "prior compose",
	}
	if err := WriteManifest(dir, a); err != nil {
		t.Fatalf("WriteManifest A: %v", err)
	}
	aRecordedAt := a.RecordedAt

	b := &DogfoodManifest{
		Base:    ManifestBase{Ref: "main", SHA: "bbb222"},
		Layered: []LayeredEntry{{Name: "F2", SHA: "fff222", Applied: "merge"}},
		Note:    "current compose",
	}
	if err := WriteManifest(dir, b); err != nil {
		t.Fatalf("WriteManifest B: %v", err)
	}

	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.Dogfood == nil {
		t.Fatal("expected Dogfood populated")
	}
	if res.Dogfood.PreviousManifest == nil {
		t.Fatal("expected Dogfood.PreviousManifest populated after a second write")
	}
	if res.Dogfood.PreviousManifest.Base != a.Base {
		t.Errorf("prior base mismatch: got %+v want %+v", res.Dogfood.PreviousManifest.Base, a.Base)
	}
	if res.Dogfood.PreviousManifest.Note != a.Note {
		t.Errorf("prior note mismatch: got %q want %q", res.Dogfood.PreviousManifest.Note, a.Note)
	}
	if res.Dogfood.PreviousManifest.RecordedAt != aRecordedAt {
		t.Errorf("prior RecordedAt should equal A's original %q, got %q",
			aRecordedAt, res.Dogfood.PreviousManifest.RecordedAt)
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

// TestStatus_ConfigSourcesSharedAndLocalMix verifies the canonical overlay
// scenario: shared file sets default_branch, [build], [restart]; local file
// sets dogfood_branch. config_sources reports the right source for each.
func TestStatus_ConfigSourcesSharedAndLocalMix(t *testing.T) {
	const shared = `schema_version = 1
default_branch = "main"
[build]
command = ["true"]
[restart]
mechanism = "none"
`
	src, client := statusFixture(t, shared)
	// Add .iris.local.toml with dogfood_branch.
	if err := os.WriteFile(filepath.Join(src, ".iris.local.toml"),
		[]byte(`dogfood_branch = "dev"`+"\n"), 0o644); err != nil {
		t.Fatalf("write local toml: %v", err)
	}
	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.ConfigSources == nil {
		t.Fatal("expected ConfigSources non-nil")
	}
	want := map[string]string{
		"schema_version": "shared",
		"default_branch": "shared",
		"build":          "shared",
		"restart":        "shared",
		"dogfood_branch": "local",
	}
	for k, v := range want {
		got, ok := res.ConfigSources[k]
		if !ok {
			t.Errorf("missing config_sources[%q]", k)
			continue
		}
		if got != v {
			t.Errorf("config_sources[%q] = %q, want %q", k, got, v)
		}
	}
	for k := range res.ConfigSources {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected config_sources[%q] = %q", k, res.ConfigSources[k])
		}
	}
}

// TestStatus_ConfigSourcesOmitsUnsetFields verifies that fields not set in
// either file are absent from config_sources (no "none" sentinel).
func TestStatus_ConfigSourcesOmitsUnsetFields(t *testing.T) {
	src, client := statusFixture(t, tomlNone) // sets schema_version, build, restart only
	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if _, ok := res.ConfigSources["ship_ci_timeout_seconds"]; ok {
		t.Error("expected ship_ci_timeout_seconds absent from config_sources when unset")
	}
	if _, ok := res.ConfigSources["dogfood_branch"]; ok {
		t.Error("expected dogfood_branch absent from config_sources when unset")
	}
	if _, ok := res.ConfigSources["default_branch"]; ok {
		t.Error("expected default_branch absent from config_sources when unset")
	}
	if _, ok := res.ConfigSources["pre_flight"]; ok {
		t.Error("expected pre_flight absent from config_sources when unset")
	}
}

// TestStatus_ConfigSourcesReportsLegacyPlacement verifies that the legacy
// placement (dogfood_branch in shared file, no local file) reports
// dogfood_branch: "shared" so the un-migrated state is visible.
func TestStatus_ConfigSourcesReportsLegacyPlacement(t *testing.T) {
	const legacy = `schema_version = 1
dogfood_branch = "dev"
[build]
command = ["true"]
[restart]
mechanism = "none"
`
	src, client := statusFixture(t, legacy)
	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	got, ok := res.ConfigSources["dogfood_branch"]
	if !ok {
		t.Fatal("expected config_sources[dogfood_branch] present (legacy placement)")
	}
	if got != "shared" {
		t.Errorf("config_sources[dogfood_branch] = %q, want %q", got, "shared")
	}
}

// TestStatus_ConfigSourcesEmptyWhenNoFiles verifies that with no `.iris.toml`
// and no `.iris.local.toml`, config_sources is an empty object (not nil/null
// or absent) and config is null, matching the consumer-ergonomics contract.
func TestStatus_ConfigSourcesEmptyWhenNoFiles(t *testing.T) {
	src, client := statusFixture(t, "") // no .iris.toml, no .iris.local.toml
	res, err := Status(context.Background(), client, StatusInput{Path: src})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.Config != nil {
		t.Fatal("expected config nil when no files present")
	}
	if res.ConfigSources == nil {
		t.Fatal("expected ConfigSources non-nil (must be empty map, not nil) when no files present")
	}
	if len(res.ConfigSources) != 0 {
		t.Fatalf("expected empty ConfigSources, got %+v", res.ConfigSources)
	}
	// Verify it JSON-marshals as `{}`, not `null`.
	body, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"config_sources":{}`) {
		t.Errorf("expected JSON to contain \"config_sources\":{}, got: %s", string(body))
	}
	// And the missing-config-is-silent contract still holds.
	for _, w := range res.Warnings {
		if strings.Contains(w, ".iris.toml") || strings.Contains(w, "file not found") {
			t.Fatalf("expected no warning about missing .iris.toml, got: %q", w)
		}
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

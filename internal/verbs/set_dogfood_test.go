package verbs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anutron/iris/internal/argus"
)

// tomlDogfoodNone is a valid cross-target .iris.toml declaring a dogfood
// branch plus a no-op build/restart so the embedded reload sequence runs to
// completion in tests without spawning anything.
const tomlDogfoodNone = `
schema_version = 1
dogfood_branch = "dev"
[build]
command = ["true"]
[restart]
mechanism = "none"
`

// setupDogfoodRepo builds a bare origin + source clone + argus worktree, then
// commits the given .iris.toml at the source root so the working tree is clean
// for the reload pre-flight. Returns (src, wt, bare, dogfoodSHA, client).
//
// dogfoodSHA is the worktree branch's HEAD — a commit reachable in the shared
// object store of the source repo, suitable as a set_dogfood target.
//
// The .iris.toml is committed on BOTH the default branch and the worktree
// (dogfood) branch: set_dogfood now builds the composed SHA by checking out the
// dogfood branch, so that branch's tree must carry its own .iris.toml (mirroring
// real dogfooding, where the composed SHA includes the build config).
func setupDogfoodRepo(t *testing.T, slug, toml string) (src, wt, bare, dogfoodSHA string, client *argus.Client) {
	t.Helper()
	return setupDogfoodRepoFiles(t, slug, map[string]string{".iris.toml": toml})
}

// setupDogfoodRepoFiles is setupDogfoodRepo generalized to an arbitrary set of
// repo-relative files, each committed on BOTH the default branch and the
// worktree (dogfood) branch so the composed SHA carries them. Use it when a
// test needs a build script, a .gitignore (so a gitignored .iris.local.toml
// keeps the tree clean for reload pre-flight), or other fixture files alongside
// .iris.toml.
func setupDogfoodRepoFiles(t *testing.T, slug string, files map[string]string) (src, wt, bare, dogfoodSHA string, client *argus.Client) {
	t.Helper()
	setAuditDir(t)
	src, wt, bare = setupRepoWithBareAndWorktree(t, slug)
	g := gitRunner(t)

	writeCommit := func(dir string) {
		for name, content := range files {
			p := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatalf("mkdir for %s: %v", name, err)
			}
			if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
			g(dir, "add", name)
		}
		g(dir, "commit", "-m", "fixture files")
	}
	writeCommit(src)
	writeCommit(wt)

	// The worktree branch HEAD is reachable in the source repo's object store.
	dogfoodSHA = strings.TrimSpace(g(wt, "rev-parse", "HEAD"))
	client = stubArgus(t, src, wt)
	return
}

func TestSetDogfood_HappyPathSetsBranchAndReloads(t *testing.T) {
	src, _, _, sha, client := setupDogfoodRepo(t, "sd-happy", tomlDogfoodNone)
	g := gitRunner(t)
	// dev already exists, pointing at main's commit.
	g(src, "branch", "dev", "main")
	priorDev := revParse(t, src, "refs/heads/dev")

	result, err := SetDogfood(context.Background(), client, "task-sd", SetDogfoodOpts{
		Sha:      sha,
		Manifest: sampleManifest(),
	})
	if err != nil {
		t.Fatalf("SetDogfood: %v", err)
	}
	if !result.Set {
		t.Fatal("expected Set=true")
	}
	if result.DogfoodBranch != "dev" {
		t.Fatalf("DogfoodBranch: got %q want dev", result.DogfoodBranch)
	}
	if result.PreviousSHA != priorDev {
		t.Fatalf("PreviousSHA: got %q want %q", result.PreviousSHA, priorDev)
	}
	if result.NewSHA != sha {
		t.Fatalf("NewSHA: got %q want %q", result.NewSHA, sha)
	}
	if result.Reload == nil {
		t.Fatal("expected a Reload result")
	}
	// dev now points at the supplied SHA.
	if got := revParse(t, src, "refs/heads/dev"); got != sha {
		t.Fatalf("dev branch: got %q want %q", got, sha)
	}
}

func TestSetDogfood_RefusesWhenDogfoodBranchUnset(t *testing.T) {
	// .iris.toml without dogfood_branch.
	const noDogfood = `
schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "none"
`
	src, _, _, sha, client := setupDogfoodRepo(t, "sd-unset", noDogfood)
	g := gitRunner(t)
	g(src, "branch", "dev", "main")
	beforeBranch := currentBranchOrFail(t, src)
	beforeHead := headSHA(t, src)
	beforeDev := revParse(t, src, "refs/heads/dev")

	_, err := SetDogfood(context.Background(), client, "task-sd", SetDogfoodOpts{
		Sha:      sha,
		Manifest: sampleManifest(),
	})
	if err == nil {
		t.Fatal("expected refusal when dogfood_branch is unset")
	}
	if !strings.Contains(err.Error(), "dogfood_branch not configured") {
		t.Fatalf("error should explain the missing config: %v", err)
	}
	// No git mutation, no manifest written.
	if currentBranchOrFail(t, src) != beforeBranch || headSHA(t, src) != beforeHead {
		t.Fatal("source repo state changed on refusal")
	}
	if revParse(t, src, "refs/heads/dev") != beforeDev {
		t.Fatal("dev branch moved on refusal")
	}
	stateDir, _ := SourceRepoStateDir(canonicalize(src))
	if m, _ := ReadManifest(stateDir); m != nil {
		t.Fatal("manifest written on refusal")
	}
}

// TestSetDogfood_ResolvesDogfoodBranchFromLocalToml covers Bug 1: dogfood_branch
// is a local-tagged field, so a value set ONLY in the gitignored .iris.local.toml
// must be honored (set_dogfood reads the merged overlay, not the shared file
// alone — matching iris:validate_config).
func TestSetDogfood_ResolvesDogfoodBranchFromLocalToml(t *testing.T) {
	const sharedNoDogfood = `
schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "none"
`
	src, _, _, sha, client := setupDogfoodRepoFiles(t, "sd-localtoml", map[string]string{
		".iris.toml": sharedNoDogfood,
		".gitignore": ".iris.local.toml\n",
	})
	// dogfood_branch lives ONLY in the gitignored local overlay. It stays
	// untracked-but-ignored so the reload pre-flight's clean-tree check passes.
	if err := os.WriteFile(filepath.Join(src, ".iris.local.toml"), []byte(`dogfood_branch = "dev"`+"\n"), 0o644); err != nil {
		t.Fatalf("write .iris.local.toml: %v", err)
	}
	g := gitRunner(t)
	g(src, "branch", "dev", "main")

	result, err := SetDogfood(context.Background(), client, "task-sd", SetDogfoodOpts{
		Sha:      sha,
		Manifest: sampleManifest(),
	})
	if err != nil {
		t.Fatalf("SetDogfood should honor dogfood_branch from .iris.local.toml: %v", err)
	}
	if result.DogfoodBranch != "dev" {
		t.Fatalf("DogfoodBranch: got %q want dev", result.DogfoodBranch)
	}
	if got := revParse(t, src, "refs/heads/dev"); got != sha {
		t.Fatalf("dev branch: got %q want %q", got, sha)
	}
}

// TestSetDogfood_BuildDeploysComposedSHA covers Bug 2: the build must run
// against the composed dogfood SHA's tree, not the default branch's. The build
// command records the HEAD it sees into a marker file; that HEAD must be the
// composed SHA. Afterward the source repo is restored to the default branch.
func TestSetDogfood_BuildDeploysComposedSHA(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "built-head")
	toml := `
schema_version = 1
dogfood_branch = "dev"
[build]
command = ["sh", "record-build.sh"]
[build.env]
BUILD_MARKER = "` + marker + `"
[restart]
mechanism = "none"
`
	const recordBuild = "#!/bin/sh\ngit rev-parse HEAD > \"$BUILD_MARKER\"\n"
	src, _, _, sha, client := setupDogfoodRepoFiles(t, "sd-buildsha", map[string]string{
		".iris.toml":      toml,
		"record-build.sh": recordBuild,
	})
	g := gitRunner(t)
	g(src, "branch", "dev", "main")
	mainHead := headSHA(t, src)
	if sha == mainHead {
		t.Fatalf("precondition: composed SHA %q must differ from default-branch HEAD", sha)
	}

	if _, err := SetDogfood(context.Background(), client, "task-sd", SetDogfoodOpts{
		Sha:      sha,
		Manifest: sampleManifest(),
	}); err != nil {
		t.Fatalf("SetDogfood: %v", err)
	}

	built, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("build marker not written (the build did not run?): %v", err)
	}
	if builtSHA := strings.TrimSpace(string(built)); builtSHA != sha {
		t.Fatalf("build ran against %q; want the composed dogfood SHA %q (default HEAD %q)", builtSHA, sha, mainHead)
	}
	// The source repo is restored to the default branch after the build.
	if b := currentBranchOrFail(t, src); b != "main" {
		t.Fatalf("source repo left on %q; want it restored to main", b)
	}
}

func TestSetDogfood_RefusesUnreachableSHA(t *testing.T) {
	src, _, _, _, client := setupDogfoodRepo(t, "sd-unreachable", tomlDogfoodNone)
	g := gitRunner(t)
	g(src, "branch", "dev", "main")
	beforeDev := revParse(t, src, "refs/heads/dev")

	_, err := SetDogfood(context.Background(), client, "task-sd", SetDogfoodOpts{
		Sha:      "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		Manifest: sampleManifest(),
	})
	if err == nil {
		t.Fatal("expected error for unreachable SHA")
	}
	if !strings.Contains(err.Error(), "deadbeef") {
		t.Fatalf("error should name the unreachable SHA: %v", err)
	}
	if revParse(t, src, "refs/heads/dev") != beforeDev {
		t.Fatal("dev branch moved on unreachable SHA")
	}
}

func TestSetDogfood_PersistsManifestAlongsideAuditLog(t *testing.T) {
	src, _, _, sha, client := setupDogfoodRepo(t, "sd-manifest", tomlDogfoodNone)
	g := gitRunner(t)
	g(src, "branch", "dev", "main")

	if _, err := SetDogfood(context.Background(), client, "task-sd", SetDogfoodOpts{
		Sha:      sha,
		Manifest: sampleManifest(),
	}); err != nil {
		t.Fatalf("SetDogfood: %v", err)
	}

	stateDir, err := SourceRepoStateDir(canonicalize(src))
	if err != nil {
		t.Fatalf("SourceRepoStateDir: %v", err)
	}
	m, err := ReadManifest(stateDir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if m == nil {
		t.Fatal("manifest not persisted")
	}
	if m.Base.Ref != "main" {
		t.Fatalf("manifest base ref: got %q want main", m.Base.Ref)
	}
	if m.RecordedAt == "" {
		t.Fatal("manifest missing recorded_at")
	}
	// File lives under the iris audit/state dir.
	if _, err := os.Stat(filepath.Join(stateDir, DogfoodManifestFilename)); err != nil {
		t.Fatalf("manifest file missing in state dir: %v", err)
	}
}

func TestSetDogfood_ManifestWriteFailureLeavesBranchUntouched(t *testing.T) {
	src, _, _, sha, client := setupDogfoodRepo(t, "sd-manifest-fail", tomlDogfoodNone)
	g := gitRunner(t)
	g(src, "branch", "dev", "main")
	beforeDev := revParse(t, src, "refs/heads/dev")

	// Point the audit/state dir at a path whose parent is a regular file, so
	// WriteManifest's MkdirAll fails before any git mutation.
	notADir := filepath.Join(t.TempDir(), "iam-a-file")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	t.Setenv(AuditDirEnv, filepath.Join(notADir, "audit"))

	_, err := SetDogfood(context.Background(), client, "task-sd", SetDogfoodOpts{
		Sha:      sha,
		Manifest: sampleManifest(),
	})
	if err == nil {
		t.Fatal("expected error when the manifest write fails")
	}
	if revParse(t, src, "refs/heads/dev") != beforeDev {
		t.Fatal("dev branch reset despite manifest write failure")
	}
}

func TestSetDogfood_BranchResetFailureLeavesManifestAhead(t *testing.T) {
	src, _, _, sha, client := setupDogfoodRepo(t, "sd-reset-fail", tomlDogfoodNone)
	g := gitRunner(t)
	g(src, "branch", "dev", "main")
	// Check dev out in the source repo so `git branch -f dev <sha>` is refused
	// by git (cannot force-update a checked-out branch).
	g(src, "checkout", "dev")
	beforeDev := revParse(t, src, "refs/heads/dev")

	_, err := SetDogfood(context.Background(), client, "task-sd", SetDogfoodOpts{
		Sha:      sha,
		Manifest: sampleManifest(),
	})
	if err == nil {
		t.Fatal("expected error when the branch reset fails")
	}
	// Branch unchanged...
	if revParse(t, src, "refs/heads/dev") != beforeDev {
		t.Fatal("dev branch moved despite reset failure")
	}
	// ...but the manifest is ahead (written before the failed reset).
	stateDir, _ := SourceRepoStateDir(canonicalize(src))
	m, _ := ReadManifest(stateDir)
	if m == nil {
		t.Fatal("manifest should have been written before the reset failed")
	}
}

func TestSetDogfood_CreatesBranchIfMissing(t *testing.T) {
	src, _, _, sha, client := setupDogfoodRepo(t, "sd-create", tomlDogfoodNone)
	// No dev branch created.
	if _, err := runGit(context.Background(), src, "rev-parse", "--verify", "--quiet", "refs/heads/dev"); err == nil {
		t.Fatal("precondition: dev should not exist yet")
	}

	result, err := SetDogfood(context.Background(), client, "task-sd", SetDogfoodOpts{
		Sha:      sha,
		Manifest: sampleManifest(),
	})
	if err != nil {
		t.Fatalf("SetDogfood: %v", err)
	}
	if result.PreviousSHA != "" {
		t.Fatalf("PreviousSHA should be empty for a freshly created branch, got %q", result.PreviousSHA)
	}
	if got := revParse(t, src, "refs/heads/dev"); got != sha {
		t.Fatalf("dev branch: got %q want %q", got, sha)
	}
}

func TestSetDogfood_RefusesUnknownTask(t *testing.T) {
	t.Parallel()
	client := stubArgusTaskNotFound(t)
	_, err := SetDogfood(context.Background(), client, "ghost", SetDogfoodOpts{
		Sha:      "abc123",
		Manifest: sampleManifest(),
	})
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected error naming task id, got: %v", err)
	}
}

func TestSetDogfood_LockSerializesConcurrentCalls(t *testing.T) {
	src, _, _, sha, client := setupDogfoodRepo(t, "sd-lock", tomlDogfoodNone)
	g := gitRunner(t)
	g(src, "branch", "dev", "main")

	canon, _ := filepath.EvalSymlinks(src)
	mu := lockSourceRepo(canon)
	released := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := SetDogfood(context.Background(), client, "task-sd", SetDogfoodOpts{
			Sha:      sha,
			Manifest: sampleManifest(),
		})
		if err != nil {
			t.Errorf("SetDogfood under lock: %v", err)
		}
		close(released)
	}()
	select {
	case <-released:
		mu.Unlock()
		t.Fatal("SetDogfood did not block on the held source-repo lock")
	case <-time.After(150 * time.Millisecond):
		// expected: parked on lockSourceRepo
	}
	mu.Unlock()
	wg.Wait()
}

// TestSetDogfood_ResultMarshalsAsJSON covers the "Direct CLI invocation mirrors
// MCP" scenario at the data layer: the same verbs.SetDogfood result serializes
// to the documented pretty-printed JSON shape both surfaces print.
func TestSetDogfood_ResultMarshalsAsJSON(t *testing.T) {
	src, _, _, sha, client := setupDogfoodRepo(t, "sd-json", tomlDogfoodNone)
	g := gitRunner(t)
	g(src, "branch", "dev", "main")

	result, err := SetDogfood(context.Background(), client, "task-sd", SetDogfoodOpts{
		Sha:      sha,
		Manifest: sampleManifest(),
	})
	if err != nil {
		t.Fatalf("SetDogfood: %v", err)
	}
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	for _, key := range []string{`"set"`, `"dogfood_branch"`, `"previous_sha"`, `"new_sha"`, `"reload"`} {
		if !strings.Contains(string(body), key) {
			t.Fatalf("result JSON missing %s:\n%s", key, body)
		}
	}
}

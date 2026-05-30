package verbs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anutron/iris/internal/argus"
)

// setupRecomposeRepo scaffolds a source repo wired for a pr-auto ship followed
// by a post-ship dogfood re-compose: a committed dogfood .iris.toml (branch
// "dev", no-op build/restart) on main pushed to origin, plus an argus worktree.
// Feature branches, the composed dev branch, and the manifest are built per-test
// on top of this. Returns (src, wt, bare, client).
func setupRecomposeRepo(t *testing.T, slug string) (src, wt, bare string, client *argus.Client) {
	t.Helper()
	setAuditDir(t)
	src, wt, bare = setupRepoWithBareAndWorktree(t, slug)
	g := gitRunner(t)

	if err := os.WriteFile(filepath.Join(src, ".iris.toml"), []byte(tomlDogfoodNone), 0o644); err != nil {
		t.Fatalf("write .iris.toml: %v", err)
	}
	g(src, "add", ".iris.toml")
	g(src, "commit", "-m", "fixture: .iris.toml")
	// Push main so the re-compose base (origin/main) includes the .iris.toml
	// commit the feature branches branch off of.
	g(src, "push", "origin", "main")
	g(src, "checkout", "main")

	client = stubArgus(t, src, wt)
	return
}

// writeRecomposeFile writes content to relPath under dir, failing the test on
// error. A small wrapper used by the per-test feature-branch fixtures.
func writeRecomposeFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, relPath), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}

// TestShipFeature_RecomposeDropsShippedFeature covers the "Recompose drops the
// shipped feature from the manifest" scenario: dogfood = main + F2 + F3, ship
// F2, expect dogfood re-composed to main + F3 with the manifest dropping F2.
func TestShipFeature_RecomposeDropsShippedFeature(t *testing.T) {
	src, _, _, client := setupRecomposeRepo(t, "recompose-drops")
	g := gitRunner(t)
	ctx := context.Background()

	mainSHA := revParse(t, src, "refs/heads/main")

	// feature/F2 adds fileF2.txt; feature/F3 adds fileF3.txt (independent).
	g(src, "checkout", "-b", "feature/F2", "main")
	writeRecomposeFile(t, src, "fileF2.txt", "f2\n")
	g(src, "add", "fileF2.txt")
	g(src, "commit", "-m", "F2")
	f2SHA := revParse(t, src, "refs/heads/feature/F2")

	g(src, "checkout", "-b", "feature/F3", "main")
	writeRecomposeFile(t, src, "fileF3.txt", "f3\n")
	g(src, "add", "fileF3.txt")
	g(src, "commit", "-m", "F3")
	f3SHA := revParse(t, src, "refs/heads/feature/F3")

	// Compose dev = main + F2 + F3.
	g(src, "checkout", "-b", "dev", "main")
	g(src, "cherry-pick", f2SHA)
	g(src, "cherry-pick", f3SHA)
	oldDevSHA := revParse(t, src, "refs/heads/dev")
	g(src, "checkout", "main") // dev must not be checked out for the reset

	stateDir, err := SourceRepoStateDir(canonicalize(src))
	if err != nil {
		t.Fatalf("SourceRepoStateDir: %v", err)
	}
	if err := WriteManifest(stateDir, &DogfoodManifest{
		Base: ManifestBase{Ref: "main", SHA: mainSHA},
		Layered: []LayeredEntry{
			{Name: "feature/F2", SHA: f2SHA, Applied: "cherry-pick"},
			{Name: "feature/F3", SHA: f3SHA, Applied: "cherry-pick"},
		},
		Note: "dogfooding F2 + F3",
	}); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	writeFakeGH(t, fakeGHPRAutoBody(checksAllPass))

	result, err := ShipFeature(ctx, client, "task-ship", ShipFeatureOpts{
		Branch: "feature/F2",
		Via:    "pr-auto",
	})
	if err != nil {
		t.Fatalf("ShipFeature: %v", err)
	}
	if !result.Merged {
		t.Fatal("expected Merged=true")
	}
	if result.Recompose == nil {
		t.Fatal("expected a Recompose result")
	}
	if !result.Recompose.Attempted || !result.Recompose.Succeeded {
		t.Fatalf("expected attempted+succeeded re-compose; got %+v", result.Recompose)
	}
	if result.Recompose.NewSHA == "" {
		t.Fatal("expected a non-empty re-composed NewSHA")
	}
	if result.Recompose.NewSHA == oldDevSHA {
		t.Fatalf("dogfood SHA should change after dropping F2; still %q", oldDevSHA)
	}
	for _, w := range result.Warnings {
		if w.Code == "recompose_error" || w.Code == "recompose_skipped" {
			t.Fatalf("unexpected warning on successful re-compose: %+v", w)
		}
	}

	// dev now points at the re-composed tip.
	if got := revParse(t, src, "refs/heads/dev"); got != result.Recompose.NewSHA {
		t.Fatalf("dev branch: got %q want %q", got, result.Recompose.NewSHA)
	}
	if got := revParse(t, src, "refs/heads/dev"); got == oldDevSHA {
		t.Fatal("dev branch SHA did not change after re-compose")
	}

	// Manifest now contains only F3.
	m, err := ReadManifest(stateDir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if m == nil {
		t.Fatal("manifest missing after re-compose")
	}
	if len(m.Layered) != 1 || m.Layered[0].Name != "feature/F3" {
		t.Fatalf("manifest layered should be [feature/F3]; got %+v", m.Layered)
	}
	if m.Base.SHA != mainSHA {
		t.Fatalf("manifest base sha: got %q want origin/main %q", m.Base.SHA, mainSHA)
	}
	if m.Note != "dogfooding F2 + F3" {
		t.Fatalf("manifest note should be preserved; got %q", m.Note)
	}
}

// TestShipFeature_RecomposePreservesOnConflict covers the "Recompose preserves
// dogfood state on conflict" scenario: F3 depends on F2's file, so once F2 is
// shipped and dropped, re-applying F3 against the F2-less base conflicts. The
// dogfood branch and manifest must be left exactly as they were.
func TestShipFeature_RecomposePreservesOnConflict(t *testing.T) {
	src, _, _, client := setupRecomposeRepo(t, "recompose-conflict")
	g := gitRunner(t)
	ctx := context.Background()

	mainSHA := revParse(t, src, "refs/heads/main")

	// feature/F2 adds shared.txt; feature/F3 (built atop F2) modifies it. Once
	// F2 is dropped, cherry-picking F3 onto a base without shared.txt conflicts.
	g(src, "checkout", "-b", "feature/F2", "main")
	writeRecomposeFile(t, src, "shared.txt", "f2\n")
	g(src, "add", "shared.txt")
	g(src, "commit", "-m", "F2 adds shared.txt")
	f2SHA := revParse(t, src, "refs/heads/feature/F2")

	g(src, "checkout", "-b", "feature/F3", "feature/F2")
	writeRecomposeFile(t, src, "shared.txt", "f2+f3\n")
	g(src, "add", "shared.txt")
	g(src, "commit", "-m", "F3 modifies shared.txt")
	f3SHA := revParse(t, src, "refs/heads/feature/F3")

	g(src, "checkout", "-b", "dev", "main")
	g(src, "cherry-pick", f2SHA)
	g(src, "cherry-pick", f3SHA)
	oldDevSHA := revParse(t, src, "refs/heads/dev")
	g(src, "checkout", "main")

	stateDir, err := SourceRepoStateDir(canonicalize(src))
	if err != nil {
		t.Fatalf("SourceRepoStateDir: %v", err)
	}
	if err := WriteManifest(stateDir, &DogfoodManifest{
		Base: ManifestBase{Ref: "main", SHA: mainSHA},
		Layered: []LayeredEntry{
			{Name: "feature/F2", SHA: f2SHA, Applied: "cherry-pick"},
			{Name: "feature/F3", SHA: f3SHA, Applied: "cherry-pick"},
		},
		Note: "dogfooding F2 + F3",
	}); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	manifestFile := filepath.Join(stateDir, DogfoodManifestFilename)
	beforeContent, err := os.ReadFile(manifestFile)
	if err != nil {
		t.Fatalf("read manifest before: %v", err)
	}
	beforeStat, err := os.Stat(manifestFile)
	if err != nil {
		t.Fatalf("stat manifest before: %v", err)
	}

	writeFakeGH(t, fakeGHPRAutoBody(checksAllPass))

	result, err := ShipFeature(ctx, client, "task-ship", ShipFeatureOpts{
		Branch: "feature/F2",
		Via:    "pr-auto",
	})
	if err != nil {
		t.Fatalf("ShipFeature: %v", err)
	}
	if !result.Merged {
		t.Fatal("expected the merge to have happened before the failed re-compose")
	}
	if result.Recompose == nil {
		t.Fatal("expected a Recompose result")
	}
	if !result.Recompose.Attempted {
		t.Fatal("expected Recompose.Attempted=true on a conflict")
	}
	if result.Recompose.Succeeded {
		t.Fatal("expected Recompose.Succeeded=false on a conflict")
	}
	if result.Recompose.Conflict == nil {
		t.Fatal("expected a conflict descriptor")
	}
	if result.Recompose.Conflict.Branch != "feature/F3" {
		t.Fatalf("conflict branch: got %q want feature/F3", result.Recompose.Conflict.Branch)
	}

	// Dogfood branch SHA unchanged.
	if got := revParse(t, src, "refs/heads/dev"); got != oldDevSHA {
		t.Fatalf("dev branch moved despite a re-compose conflict: got %q want %q", got, oldDevSHA)
	}

	// Manifest file unchanged: identical content and modification time.
	afterContent, err := os.ReadFile(manifestFile)
	if err != nil {
		t.Fatalf("read manifest after: %v", err)
	}
	if string(afterContent) != string(beforeContent) {
		t.Fatalf("manifest content changed on conflict:\nbefore:\n%s\nafter:\n%s", beforeContent, afterContent)
	}
	afterStat, err := os.Stat(manifestFile)
	if err != nil {
		t.Fatalf("stat manifest after: %v", err)
	}
	if !afterStat.ModTime().Equal(beforeStat.ModTime()) {
		t.Fatalf("manifest mtime changed on conflict: before=%v after=%v", beforeStat.ModTime(), afterStat.ModTime())
	}
}

// TestShipFeature_RecomposeSkippedNoManifest covers the "Recompose is skipped
// when no dogfood manifest exists" scenario: no manifest file means attempted
// is false, no warnings, and nothing is written.
func TestShipFeature_RecomposeSkippedNoManifest(t *testing.T) {
	src, _, _, client := setupRecomposeRepo(t, "recompose-nomanifest")
	g := gitRunner(t)
	ctx := context.Background()

	// A dev branch exists but there is no manifest describing it.
	g(src, "branch", "dev", "main")
	beforeDev := revParse(t, src, "refs/heads/dev")

	g(src, "checkout", "-b", "feature/F2", "main")
	writeRecomposeFile(t, src, "fileF2.txt", "f2\n")
	g(src, "add", "fileF2.txt")
	g(src, "commit", "-m", "F2")
	g(src, "checkout", "main")

	stateDir, err := SourceRepoStateDir(canonicalize(src))
	if err != nil {
		t.Fatalf("SourceRepoStateDir: %v", err)
	}

	writeFakeGH(t, fakeGHPRAutoBody(checksAllPass))

	result, err := ShipFeature(ctx, client, "task-ship", ShipFeatureOpts{
		Branch: "feature/F2",
		Via:    "pr-auto",
	})
	if err != nil {
		t.Fatalf("ShipFeature: %v", err)
	}
	if result.Recompose == nil {
		t.Fatal("expected a Recompose result")
	}
	if result.Recompose.Attempted {
		t.Fatalf("re-compose must be skipped when no manifest exists; got %+v", result.Recompose)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("no-manifest skip must emit no warnings; got %+v", result.Warnings)
	}
	// No manifest was written.
	if m, _ := ReadManifest(stateDir); m != nil {
		t.Fatal("a manifest was written despite the no-manifest skip")
	}
	// dev untouched.
	if got := revParse(t, src, "refs/heads/dev"); got != beforeDev {
		t.Fatalf("dev branch moved on a no-manifest skip: got %q want %q", got, beforeDev)
	}
}

// TestShipFeature_RecomposeSkippedBranchNotInManifest covers the "Recompose is
// skipped when shipped branch was not in the manifest" scenario: the manifest
// lists only F3; shipping F2 leaves dogfood + manifest untouched and emits
// exactly one structured warning.
func TestShipFeature_RecomposeSkippedBranchNotInManifest(t *testing.T) {
	src, _, _, client := setupRecomposeRepo(t, "recompose-notinmanifest")
	g := gitRunner(t)
	ctx := context.Background()

	mainSHA := revParse(t, src, "refs/heads/main")

	g(src, "checkout", "-b", "feature/F3", "main")
	writeRecomposeFile(t, src, "fileF3.txt", "f3\n")
	g(src, "add", "fileF3.txt")
	g(src, "commit", "-m", "F3")
	f3SHA := revParse(t, src, "refs/heads/feature/F3")

	// feature/F2 is the branch we ship; it is NOT in the manifest.
	g(src, "checkout", "-b", "feature/F2", "main")
	writeRecomposeFile(t, src, "fileF2.txt", "f2\n")
	g(src, "add", "fileF2.txt")
	g(src, "commit", "-m", "F2")

	// dev = main + F3 only.
	g(src, "checkout", "-b", "dev", "main")
	g(src, "cherry-pick", f3SHA)
	oldDevSHA := revParse(t, src, "refs/heads/dev")
	g(src, "checkout", "main")

	stateDir, err := SourceRepoStateDir(canonicalize(src))
	if err != nil {
		t.Fatalf("SourceRepoStateDir: %v", err)
	}
	if err := WriteManifest(stateDir, &DogfoodManifest{
		Base:    ManifestBase{Ref: "main", SHA: mainSHA},
		Layered: []LayeredEntry{{Name: "feature/F3", SHA: f3SHA, Applied: "cherry-pick"}},
		Note:    "dogfooding F3",
	}); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	manifestFile := filepath.Join(stateDir, DogfoodManifestFilename)
	beforeContent, err := os.ReadFile(manifestFile)
	if err != nil {
		t.Fatalf("read manifest before: %v", err)
	}

	writeFakeGH(t, fakeGHPRAutoBody(checksAllPass))

	result, err := ShipFeature(ctx, client, "task-ship", ShipFeatureOpts{
		Branch: "feature/F2",
		Via:    "pr-auto",
	})
	if err != nil {
		t.Fatalf("ShipFeature: %v", err)
	}
	if result.Recompose == nil {
		t.Fatal("expected a Recompose result")
	}
	if result.Recompose.Attempted {
		t.Fatalf("re-compose must be skipped when shipped branch is not layered; got %+v", result.Recompose)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected exactly one warning; got %+v", result.Warnings)
	}
	if result.Warnings[0].Code != "recompose_skipped" {
		t.Fatalf("warning code: got %q want recompose_skipped", result.Warnings[0].Code)
	}
	if !strings.Contains(result.Warnings[0].Message, "feature/F2") {
		t.Fatalf("warning should name the shipped branch: %q", result.Warnings[0].Message)
	}

	// dev untouched.
	if got := revParse(t, src, "refs/heads/dev"); got != oldDevSHA {
		t.Fatalf("dev branch moved on a not-in-manifest skip: got %q want %q", got, oldDevSHA)
	}
	// Manifest unchanged.
	afterContent, err := os.ReadFile(manifestFile)
	if err != nil {
		t.Fatalf("read manifest after: %v", err)
	}
	if string(afterContent) != string(beforeContent) {
		t.Fatalf("manifest changed on a not-in-manifest skip:\nbefore:\n%s\nafter:\n%s", beforeContent, afterContent)
	}
}

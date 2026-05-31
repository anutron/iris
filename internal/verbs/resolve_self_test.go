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

	"github.com/anutron/iris/internal/argus"
)

// setupRepoOnly creates a bare origin + clone (no worktree). Used for
// self-target tests where iris IS the source repo's "worktree."
func setupRepoOnly(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "origin.git")
	src := filepath.Join(tmp, "src")
	g := gitRunner(t)
	g("", "init", "--bare", "-b", "main", bare)
	g("", "clone", bare, src)
	g(src, "config", "user.email", "x@y.z")
	g(src, "config", "user.name", "x")
	g(src, "commit", "--allow-empty", "-m", "initial")
	g(src, "push", "-u", "origin", "main")
	g(src, "remote", "set-head", "origin", "main")
	return src
}

func TestResolveSelf_WalksUpToGitRoot(t *testing.T) {
	src := setupRepoOnly(t)
	// Place a fake "binary" under bin/iris within src.
	binDir := filepath.Join(src, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	bin := filepath.Join(binDir, "iris")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write bin: %v", err)
	}

	// Pretend iris is running from that binary.
	old := executable
	executable = func() (string, error) { return bin, nil }
	t.Cleanup(func() { executable = old })

	resolved, err := ResolveSelf(context.Background())
	if err != nil {
		t.Fatalf("resolve self: %v", err)
	}
	wantSrc, _ := filepath.EvalSymlinks(src)
	if resolved.SourceRepo != wantSrc {
		t.Fatalf("expected SourceRepo=%s, got %s", wantSrc, resolved.SourceRepo)
	}
	if resolved.SourceRepo != resolved.WorktreePath {
		t.Fatalf("expected WorktreePath == SourceRepo, got %s vs %s",
			resolved.WorktreePath, resolved.SourceRepo)
	}
	if resolved.Task != nil {
		t.Fatalf("expected Task=nil for self-resolve, got %+v", resolved.Task)
	}
}

func TestResolveSelf_FollowsSymlinks(t *testing.T) {
	src := setupRepoOnly(t)
	binReal := filepath.Join(src, "bin", "iris")
	if err := os.MkdirAll(filepath.Dir(binReal), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(binReal, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Symlink elsewhere; iris should follow it back into src/.
	link := filepath.Join(t.TempDir(), "iris-link")
	if err := os.Symlink(binReal, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	old := executable
	executable = func() (string, error) { return link, nil }
	t.Cleanup(func() { executable = old })

	resolved, err := ResolveSelf(context.Background())
	if err != nil {
		t.Fatalf("resolve self: %v", err)
	}
	wantSrc, _ := filepath.EvalSymlinks(src)
	if resolved.SourceRepo != wantSrc {
		t.Fatalf("expected %s, got %s", wantSrc, resolved.SourceRepo)
	}
}

func TestResolveSelf_NoGitDirReturnsError(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "iris")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	old := executable
	executable = func() (string, error) { return bin, nil }
	t.Cleanup(func() { executable = old })

	_, err := ResolveSelf(context.Background())
	if err == nil {
		t.Fatal("expected error when no .git directory present")
	}
	if !strings.Contains(err.Error(), ".git") {
		t.Fatalf("expected error to mention .git, got: %v", err)
	}
}

func TestResolveTarget_BothInputsAmbiguous(t *testing.T) {
	t.Parallel()
	_, err := ResolveTarget(context.Background(), nil, "tid", "/path")
	if err == nil {
		t.Fatal("expected ambiguous error")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous in error, got: %v", err)
	}
}

func TestResolveTarget_NoInputsResolvesSelf(t *testing.T) {
	src := setupRepoOnly(t)
	binDir := filepath.Join(src, "bin")
	_ = os.MkdirAll(binDir, 0o755)
	bin := filepath.Join(binDir, "iris")
	_ = os.WriteFile(bin, []byte("x"), 0o755)
	old := executable
	executable = func() (string, error) { return bin, nil }
	t.Cleanup(func() { executable = old })

	resolved, err := ResolveTarget(context.Background(), nil, "", "")
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	wantSrc, _ := filepath.EvalSymlinks(src)
	if resolved.SourceRepo != wantSrc {
		t.Fatalf("expected %s, got %s", wantSrc, resolved.SourceRepo)
	}
}

func TestResolvePath_AllowlistEnforced(t *testing.T) {
	t.Parallel()
	src, _ := setupRepoWithWorktree(t, "rp-allow")

	// Stub argus with the wrong allowlist.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/projects/full" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"name": "other", "path": "/some/other"}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	client := argus.New(srv.URL, "stub-token")

	_, err := ResolvePath(context.Background(), client, src)
	if err == nil {
		t.Fatal("expected allowlist refusal")
	}
	if !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("expected allowlist error, got: %v", err)
	}
}

func TestResolvePath_HappyPath(t *testing.T) {
	t.Parallel()
	src, _ := setupRepoWithWorktree(t, "rp-happy")
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
	client := argus.New(srv.URL, "stub-token")

	resolved, err := ResolvePath(context.Background(), client, src)
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	if resolved.SourceRepo != canon {
		t.Fatalf("expected canon source repo, got %s", resolved.SourceRepo)
	}
}

func TestEqualSourceRepos(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	canon, _ := filepath.EvalSymlinks(dir)
	if !EqualSourceRepos(dir, canon) {
		t.Fatalf("expected equality after canonicalization: %s vs %s", dir, canon)
	}
	if EqualSourceRepos(dir, "/some/other/repo") {
		t.Fatal("expected inequality")
	}
}

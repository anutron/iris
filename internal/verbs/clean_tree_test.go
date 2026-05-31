package verbs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckCleanTree_IgnoresIrisLocalToml covers the dogfood-bootstrap fix:
// reload/publish pre-flight must not treat iris's own .iris.local.toml (written
// by set_local_config) as a dirty tree, but a genuine change still refuses.
func TestCheckCleanTree_IgnoresIrisLocalToml(t *testing.T) {
	src, _, _ := setupRepoWithBareAndWorktree(t, "cct-irislocal")
	ctx := context.Background()

	// Baseline: a fresh repo is clean.
	if err := checkCleanTree(ctx, src); err != nil {
		t.Fatalf("baseline should be clean: %v", err)
	}

	// Only an untracked .iris.local.toml at the root (default branch does not
	// gitignore it yet) → still treated as clean.
	if err := os.WriteFile(filepath.Join(src, ".iris.local.toml"), []byte(`dogfood_branch = "dev"`+"\n"), 0o644); err != nil {
		t.Fatalf("write .iris.local.toml: %v", err)
	}
	if err := checkCleanTree(ctx, src); err != nil {
		t.Fatalf("untracked .iris.local.toml should be ignored by the clean-tree check, got: %v", err)
	}

	// A real uncommitted change alongside it → dirty again, and the reported
	// paths name the real change but exclude .iris.local.toml.
	if err := os.WriteFile(filepath.Join(src, "real.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write real.txt: %v", err)
	}
	err := checkCleanTree(ctx, src)
	if err == nil {
		t.Fatal("a genuine uncommitted change should still refuse")
	}
	if strings.Contains(err.Error(), ".iris.local.toml") {
		t.Fatalf("dirty report should exclude .iris.local.toml: %v", err)
	}
	if !strings.Contains(err.Error(), "real.txt") {
		t.Fatalf("dirty report should name the real change: %v", err)
	}
}

package verbs

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBranchCreate_HappyPath(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "bc-happy")
	client := stubArgus(t, src, wt)

	priorHead := headSHA(t, src)
	priorBranch := currentBranchOrFail(t, src)

	result, err := BranchCreate(context.Background(), BranchCreateInput{
		Client: client, TaskID: "task-bc", Name: "hotfix/foo", BaseRef: "origin/main",
	})
	if err != nil {
		t.Fatalf("branch_create: %v", err)
	}
	if !result.Created {
		t.Fatal("expected Created=true")
	}
	if result.Branch != "hotfix/foo" {
		t.Fatalf("unexpected branch: %q", result.Branch)
	}
	if result.BaseRef != "origin/main" {
		t.Fatalf("unexpected base_ref: %q", result.BaseRef)
	}
	want := revParse(t, src, "refs/heads/hotfix/foo")
	if result.SHA != want {
		t.Fatalf("SHA mismatch: got %q want %q", result.SHA, want)
	}
	if headSHA(t, src) != priorHead {
		t.Fatal("source repo HEAD moved; branch_create must not change current checkout")
	}
	if currentBranchOrFail(t, src) != priorBranch {
		t.Fatalf("source repo branch changed: %q -> %q", priorBranch, currentBranchOrFail(t, src))
	}
}

func TestBranchCreate_RefusesEmptyName(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "bc-empty-name")
	client := stubArgus(t, src, wt)
	_, err := BranchCreate(context.Background(), BranchCreateInput{
		Client: client, TaskID: "t", Name: "", BaseRef: "origin/main",
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Fatalf("error should mention name field: %v", err)
	}
}

func TestBranchCreate_RefusesEmptyBaseRef(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "bc-empty-base")
	client := stubArgus(t, src, wt)
	_, err := BranchCreate(context.Background(), BranchCreateInput{
		Client: client, TaskID: "t", Name: "feature/x", BaseRef: "",
	})
	if err == nil {
		t.Fatal("expected error for empty base_ref")
	}
	if !strings.Contains(err.Error(), "base_ref") {
		t.Fatalf("error should mention base_ref field: %v", err)
	}
}

func TestBranchCreate_RefusesLeadingDashName(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "bc-dash-name")
	client := stubArgus(t, src, wt)
	_, err := BranchCreate(context.Background(), BranchCreateInput{
		Client: client, TaskID: "t", Name: "--upload-pack=evil", BaseRef: "origin/main",
	})
	if err == nil {
		t.Fatal("expected error for leading-dash name")
	}
	if !strings.Contains(err.Error(), "invalid branch name") {
		t.Fatalf("error should say 'invalid branch name': %v", err)
	}
}

func TestBranchCreate_RefusesLeadingDashBaseRef(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "bc-dash-base")
	client := stubArgus(t, src, wt)
	_, err := BranchCreate(context.Background(), BranchCreateInput{
		Client: client, TaskID: "t", Name: "feature/x", BaseRef: "--upload-pack=evil",
	})
	if err == nil {
		t.Fatal("expected error for leading-dash base_ref")
	}
	if !strings.Contains(err.Error(), "invalid base_ref") {
		t.Fatalf("error should say 'invalid base_ref': %v", err)
	}
}

func TestBranchCreate_RefusesDefaultBranch(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "bc-default")
	client := stubArgus(t, src, wt)
	for _, name := range []string{"main", "master"} {
		_, err := BranchCreate(context.Background(), BranchCreateInput{
			Client: client, TaskID: "t", Name: name, BaseRef: "origin/main",
		})
		if err == nil {
			t.Fatalf("expected refusal for name=%q", name)
		}
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("error should name the refused branch %q: %v", name, err)
		}
	}
}

func TestBranchCreate_RefusesInvalidRefName(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "bc-invalid")
	client := stubArgus(t, src, wt)
	_, err := BranchCreate(context.Background(), BranchCreateInput{
		// double-dot is invalid in a git ref
		Client: client, TaskID: "t", Name: "bad..name", BaseRef: "origin/main",
	})
	if err == nil {
		t.Fatal("expected error for invalid ref name")
	}
	if !strings.Contains(err.Error(), "bad..name") {
		t.Fatalf("error should name the invalid ref: %v", err)
	}
}

func TestBranchCreate_RefusesExistingBranch(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "bc-exists")
	client := stubArgus(t, src, wt)
	g := gitRunner(t)
	g(src, "branch", "already-here", "origin/main")

	_, err := BranchCreate(context.Background(), BranchCreateInput{
		Client: client, TaskID: "t", Name: "already-here", BaseRef: "origin/main",
	})
	if err == nil {
		t.Fatal("expected error for existing branch")
	}
	if !strings.Contains(err.Error(), "already-here") {
		t.Fatalf("error should name the conflicting branch: %v", err)
	}
}

func TestBranchCreate_RefusesUnresolvableBaseRef(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "bc-unresolvable")
	client := stubArgus(t, src, wt)
	_, err := BranchCreate(context.Background(), BranchCreateInput{
		Client: client, TaskID: "t", Name: "feature/x", BaseRef: "deadbeefcafe",
	})
	if err == nil {
		t.Fatal("expected error for unresolvable base_ref")
	}
}

func TestBranchCreate_RefusesUnknownTask(t *testing.T) {
	client := stubArgusTaskNotFound(t)
	_, err := BranchCreate(context.Background(), BranchCreateInput{
		Client: client, TaskID: "ghost", Name: "feature/x", BaseRef: "origin/main",
	})
	if err == nil {
		t.Fatal("expected error for unknown task")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("error should mention task id: %v", err)
	}
}

func TestBranchCreate_LockSerializesConcurrentCalls(t *testing.T) {
	src, wt, _ := setupRepoWithBareAndWorktree(t, "bc-lock")
	client := stubArgus(t, src, wt)

	// Pre-grab the lock keyed by the same canonical path verbs.Resolve
	// produces (EvalSymlinks turns macOS /var into /private/var).
	canon, _ := filepath.EvalSymlinks(src)
	mu := lockSourceRepo(canon)
	released := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := BranchCreate(context.Background(), BranchCreateInput{
			Client: client, TaskID: "t", Name: "lock-test", BaseRef: "origin/main",
		})
		if err != nil {
			t.Errorf("branch_create under lock: %v", err)
		}
		close(released)
	}()

	select {
	case <-released:
		mu.Unlock()
		t.Fatal("branch_create did not block on held lock")
	case <-time.After(150 * time.Millisecond):
		// expected; the goroutine is parked on lockSourceRepo
	}
	mu.Unlock()
	wg.Wait()
}

// currentBranchOrFail reads `git rev-parse --abbrev-ref HEAD`.
func currentBranchOrFail(t *testing.T, dir string) string {
	t.Helper()
	g := gitRunner(t)
	out := g(dir, "rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(out)
}

// revParse reads `git rev-parse <ref>` and trims.
func revParse(t *testing.T, dir, ref string) string {
	t.Helper()
	g := gitRunner(t)
	return strings.TrimSpace(g(dir, "rev-parse", ref))
}

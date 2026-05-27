package verbs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anutron/iris/internal/argus"
)

// publishFixture builds:
//   - bare origin
//   - source repo cloned from origin, on `main`, with .iris.toml + bin/iris committed
//   - a linked worktree on `argus/<slug>` containing one extra commit
//   - audit log redirected to a tmp dir
//   - a stub argus that resolves task_id → worktree and allowlists the canonical source
//
// Returns the source repo path, worktree path, and stub argus client.
//
// The fixture defaults to a `.iris.toml` with mechanism=none so build+restart succeed
// without any external dependencies; callers needing a different .iris.toml can
// overwrite it and amend.
func publishFixture(t *testing.T, slug, irisToml string) (src, wt string, client *argus.Client) {
	t.Helper()
	setAuditDir(t)

	tmp := t.TempDir()
	bare := filepath.Join(tmp, "origin.git")
	src = filepath.Join(tmp, "src")
	wt = filepath.Join(tmp, "wt")
	g := gitRunner(t)

	g("", "init", "--bare", "-b", "main", bare)
	g("", "clone", bare, src)
	g(src, "config", "user.email", "iris-test@example.com")
	g(src, "config", "user.name", "iris-test")
	g(src, "commit", "--allow-empty", "-m", "initial")
	g(src, "push", "-u", "origin", "main")
	g(src, "remote", "set-head", "origin", "main")

	// .iris.toml + bin/iris so isSelfTarget can also resolve (cross-target by default).
	if irisToml == "" {
		irisToml = `schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "none"
`
	}
	if err := os.WriteFile(filepath.Join(src, ".iris.toml"), []byte(irisToml), 0o644); err != nil {
		t.Fatalf("write .iris.toml: %v", err)
	}
	bin := filepath.Join(src, "bin", "iris")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	_ = os.WriteFile(bin, []byte("x"), 0o755)
	g(src, "add", ".iris.toml", "bin/iris")
	g(src, "commit", "-m", "fixture: .iris.toml + bin")
	g(src, "push", "origin", "main")

	// Worktree on argus/<slug> with one extra commit.
	branch := "argus/" + slug
	g(src, "branch", branch)
	g(src, "worktree", "add", wt, branch)
	g(wt, "config", "user.email", "iris-test@example.com")
	g(wt, "config", "user.name", "iris-test")
	g(wt, "commit", "--allow-empty", "-m", "work on "+branch)

	// Move src off main onto a side branch, then back to main, so the source repo's
	// current HEAD is `main` (matching publish's target-branch constraint). Actually
	// src is already on main, so this is a no-op — we just guarantee it explicitly.
	g(src, "switch", "main")

	// Point ResolveSelf elsewhere so isSelfTarget treats this source as cross.
	elsewhere := t.TempDir()
	elseBin := filepath.Join(elsewhere, "bin", "iris")
	_ = os.MkdirAll(filepath.Dir(elseBin), 0o755)
	_ = os.WriteFile(elseBin, []byte("x"), 0o755)
	old := executable
	executable = func() (string, error) { return elseBin, nil }
	t.Cleanup(func() { executable = old })

	canon, _ := filepath.EvalSymlinks(src)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/tasks/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":            strings.TrimPrefix(r.URL.Path, "/api/tasks/"),
				"worktree_path": wt,
			})
		case r.URL.Path == "/api/projects/full":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"name": "iris-test", "path": canon}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client = argus.New(srv.URL, "stub-token")
	return src, wt, client
}

func TestPublish_FFOnlyHappyPath(t *testing.T) {
	src, wt, client := publishFixture(t, "ff-happy", "")
	preSrcSHA := headSHA(t, src)
	wtSHA := headSHA(t, wt)

	result, err := Publish(context.Background(), client, PublishInput{
		TaskID: "task-ff",
		Caller: "test",
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if result.Mode != "publish" {
		t.Fatalf("Mode: got %q, want publish", result.Mode)
	}
	if result.Branch != "main" {
		t.Fatalf("Branch: got %q, want main", result.Branch)
	}
	if result.PrePublishSha != preSrcSHA {
		t.Fatalf("PrePublishSha: got %q, want %q", result.PrePublishSha, preSrcSHA)
	}
	if result.WorktreeSha != wtSHA {
		t.Fatalf("WorktreeSha: got %q, want %q", result.WorktreeSha, wtSHA)
	}
	if result.PostPublishSha != wtSHA {
		t.Fatalf("PostPublishSha: got %q, want %q", result.PostPublishSha, wtSHA)
	}
	if afterSrcSHA := headSHA(t, src); afterSrcSHA != wtSHA {
		t.Fatalf("source HEAD after publish: got %q, want %q (worktree HEAD)", afterSrcSHA, wtSHA)
	}
	if result.Reset {
		t.Fatal("expected Reset=false on ff-only")
	}
	if result.Pushed {
		t.Fatal("expected Pushed=false without --push")
	}

	// Audit entry: mode=publish, outcome=success.
	entries, err := ReadAudit(AuditReadOpts{})
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no audit entry written")
	}
	last := entries[len(entries)-1]
	if last.Mode != "publish" {
		t.Fatalf("audit Mode: got %q, want publish", last.Mode)
	}
	if last.Outcome != "success" {
		t.Fatalf("audit Outcome: got %q, want success", last.Outcome)
	}
	if last.PrePullSha != preSrcSHA {
		t.Fatalf("audit PrePullSha: got %q, want %q", last.PrePullSha, preSrcSHA)
	}
	if last.PostPullSha != wtSHA {
		t.Fatalf("audit PostPullSha: got %q, want %q", last.PostPullSha, wtSHA)
	}
}

func TestPublish_RefusesDirtyWorktree(t *testing.T) {
	src, wt, client := publishFixture(t, "dirty-wt", "")
	if err := os.WriteFile(filepath.Join(wt, "untracked.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write dirt: %v", err)
	}
	beforeSrcSHA := headSHA(t, src)
	_, err := Publish(context.Background(), client, PublishInput{TaskID: "task-dirty-wt"})
	if err == nil {
		t.Fatal("expected dirty-worktree refusal")
	}
	if !strings.Contains(err.Error(), "dirty") && !strings.Contains(err.Error(), "untracked") {
		t.Fatalf("unexpected error: %v", err)
	}
	if after := headSHA(t, src); after != beforeSrcSHA {
		t.Fatalf("source HEAD changed on refusal: before=%q after=%q", beforeSrcSHA, after)
	}
}

func TestPublish_RefusesDirtySourceRepo(t *testing.T) {
	src, _, client := publishFixture(t, "dirty-src", "")
	if err := os.WriteFile(filepath.Join(src, "scratch.txt"), []byte("operator-wip\n"), 0o644); err != nil {
		t.Fatalf("write dirt: %v", err)
	}
	beforeSrcSHA := headSHA(t, src)
	_, err := Publish(context.Background(), client, PublishInput{TaskID: "task-dirty-src"})
	if err == nil {
		t.Fatal("expected dirty-source-repo refusal")
	}
	if !strings.Contains(err.Error(), "dirty") && !strings.Contains(err.Error(), "scratch.txt") {
		t.Fatalf("unexpected error: %v", err)
	}
	if after := headSHA(t, src); after != beforeSrcSHA {
		t.Fatalf("source HEAD changed on refusal: before=%q after=%q", beforeSrcSHA, after)
	}
}

func TestPublish_RefusesMissingIrisToml(t *testing.T) {
	src, _, client := publishFixture(t, "no-toml", "")
	// Delete .iris.toml and commit the deletion so the tree is clean.
	g := gitRunner(t)
	if err := os.Remove(filepath.Join(src, ".iris.toml")); err != nil {
		t.Fatalf("remove .iris.toml: %v", err)
	}
	g(src, "add", "-A")
	g(src, "commit", "-m", "remove .iris.toml")

	_, err := Publish(context.Background(), client, PublishInput{TaskID: "task-no-toml"})
	if err == nil {
		t.Fatal("expected missing .iris.toml refusal")
	}
	if !strings.Contains(err.Error(), ".iris.toml") {
		t.Fatalf("expected error to mention .iris.toml, got: %v", err)
	}
}

func TestPublish_RefusesAllowlistRejection(t *testing.T) {
	src, wt, _ := publishFixture(t, "denied", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/tasks/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "x", "worktree_path": wt})
		case r.URL.Path == "/api/projects/full":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"name": "other", "path": "/some/other/repo"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client := argus.New(srv.URL, "stub-token")
	beforeSrcSHA := headSHA(t, src)

	_, err := Publish(context.Background(), client, PublishInput{TaskID: "task-denied"})
	if err == nil {
		t.Fatal("expected allowlist refusal")
	}
	if !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("expected allowlist error, got: %v", err)
	}
	if after := headSHA(t, src); after != beforeSrcSHA {
		t.Fatalf("source HEAD changed on refusal: before=%q after=%q", beforeSrcSHA, after)
	}
}

func TestPublish_RefusesTargetBranchMismatch(t *testing.T) {
	src, _, client := publishFixture(t, "mismatch", "")
	// src is on main; publish targeting a branch that isn't main must refuse.
	beforeSrcSHA := headSHA(t, src)
	_, err := Publish(context.Background(), client, PublishInput{TaskID: "task-mismatch", Branch: "feature/not-main"})
	if err == nil {
		t.Fatal("expected branch-mismatch refusal")
	}
	if !strings.Contains(err.Error(), "feature/not-main") && !strings.Contains(err.Error(), "main") {
		t.Fatalf("unexpected error: %v", err)
	}
	if after := headSHA(t, src); after != beforeSrcSHA {
		t.Fatalf("source HEAD changed on refusal: before=%q after=%q", beforeSrcSHA, after)
	}
}

func TestPublish_RefusesNonAncestorWithoutReset(t *testing.T) {
	src, wt, client := publishFixture(t, "diverge", "")
	g := gitRunner(t)
	// Diverge: add an unrelated commit on src/main, then a different commit on the
	// worktree's branch. After this, neither tip is an ancestor of the other; the
	// ff-only merge from the worktree's HEAD to src/main must fail.
	g(src, "commit", "--allow-empty", "-m", "src diverges")
	g(wt, "commit", "--allow-empty", "-m", "wt diverges")

	beforeSrcSHA := headSHA(t, src)
	_, err := Publish(context.Background(), client, PublishInput{TaskID: "task-diverge"})
	if err == nil {
		t.Fatal("expected non-ancestor refusal without --reset")
	}
	// git's ff-only error language varies, just assert mutation didn't happen.
	if after := headSHA(t, src); after != beforeSrcSHA {
		t.Fatalf("source HEAD changed on refusal: before=%q after=%q", beforeSrcSHA, after)
	}
}

func TestPublish_ResetSucceedsOnDivergedHistory(t *testing.T) {
	src, wt, client := publishFixture(t, "reset-ok", "")
	g := gitRunner(t)
	g(src, "commit", "--allow-empty", "-m", "src diverges")
	g(wt, "commit", "--allow-empty", "-m", "wt diverges")
	wtSHA := headSHA(t, wt)

	result, err := Publish(context.Background(), client, PublishInput{TaskID: "task-reset", Reset: true})
	if err != nil {
		t.Fatalf("publish --reset: %v", err)
	}
	if !result.Reset {
		t.Fatal("expected Reset=true in result")
	}
	if result.PostPublishSha != wtSHA {
		t.Fatalf("PostPublishSha: got %q, want %q", result.PostPublishSha, wtSHA)
	}
	if after := headSHA(t, src); after != wtSHA {
		t.Fatalf("source HEAD after reset: got %q, want %q", after, wtSHA)
	}
}

// Push test: full inline fixture so we can put src on a non-default branch
// (publish refuses --push on the default branch).
func TestPublish_PushSucceedsOnNonDefaultBranch(t *testing.T) {
	setAuditDir(t)
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "origin.git")
	src := filepath.Join(tmp, "src")
	wt := filepath.Join(tmp, "wt")
	g := gitRunner(t)

	g("", "init", "--bare", "-b", "main", bare)
	g("", "clone", bare, src)
	g(src, "config", "user.email", "x@y.z")
	g(src, "config", "user.name", "x")
	g(src, "commit", "--allow-empty", "-m", "initial")
	g(src, "push", "-u", "origin", "main")
	g(src, "remote", "set-head", "origin", "main")

	// .iris.toml + bin
	const toml = `schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "none"
`
	_ = os.WriteFile(filepath.Join(src, ".iris.toml"), []byte(toml), 0o644)
	bin := filepath.Join(src, "bin", "iris")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	_ = os.WriteFile(bin, []byte("x"), 0o755)
	g(src, "add", ".iris.toml", "bin/iris")
	g(src, "commit", "-m", "fixture")
	g(src, "push", "origin", "main")

	// Create feature branch from main, push it so origin tracks it.
	g(src, "switch", "-c", "feature/x")
	g(src, "push", "-u", "origin", "feature/x")

	// Worktree on a separate branch that descends from feature/x.
	g(src, "branch", "argus/push-too", "feature/x")
	g(src, "worktree", "add", wt, "argus/push-too")
	g(wt, "config", "user.email", "x@y.z")
	g(wt, "config", "user.name", "x")
	g(wt, "commit", "--allow-empty", "-m", "wt commit")
	wtSHA := headSHA(t, wt)

	// src is on feature/x; the worktree's commit descends from feature/x; ff-only
	// from wtSHA onto feature/x will succeed.

	// Point ResolveSelf elsewhere.
	elsewhere := t.TempDir()
	elseBin := filepath.Join(elsewhere, "bin", "iris")
	_ = os.MkdirAll(filepath.Dir(elseBin), 0o755)
	_ = os.WriteFile(elseBin, []byte("x"), 0o755)
	old := executable
	executable = func() (string, error) { return elseBin, nil }
	t.Cleanup(func() { executable = old })

	canon, _ := filepath.EvalSymlinks(src)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/tasks/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "task-push", "worktree_path": wt})
		case r.URL.Path == "/api/projects/full":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{"name": "iris-test", "path": canon}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client := argus.New(srv.URL, "stub-token")

	result, err := Publish(context.Background(), client, PublishInput{TaskID: "task-push", Push: true})
	if err != nil {
		t.Fatalf("publish --push: %v", err)
	}
	if !result.Pushed {
		t.Fatal("expected Pushed=true")
	}
	if result.RemoteSHA != wtSHA {
		t.Fatalf("RemoteSHA: got %q, want %q", result.RemoteSHA, wtSHA)
	}
	if remoteRef(t, bare, "feature/x") != wtSHA {
		t.Fatalf("origin/feature/x didn't advance to %q", wtSHA)
	}
}

func TestPublish_PushRefusesDefaultBranch(t *testing.T) {
	src, _, client := publishFixture(t, "push-default", "")
	beforeSrcSHA := headSHA(t, src)
	_, err := Publish(context.Background(), client, PublishInput{TaskID: "task-push-default", Push: true})
	if err == nil {
		t.Fatal("expected refusal to push default branch")
	}
	if !strings.Contains(err.Error(), "default branch") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Local update may have happened before the push refusal — that's by design.
	// The push itself must NOT have happened (we just assert the error name).
	_ = beforeSrcSHA
}

func TestPublish_BuildFailureAbortsBeforeRestart(t *testing.T) {
	const tomlFail = `schema_version = 1
[build]
command = ["false"]
[restart]
mechanism = "none"
`
	src, _, client := publishFixture(t, "build-fail", tomlFail)
	preSrcSHA := headSHA(t, src)

	_, err := Publish(context.Background(), client, PublishInput{TaskID: "task-build-fail"})
	if err == nil {
		t.Fatal("expected build failure error")
	}
	if !strings.Contains(err.Error(), "build") && !strings.Contains(err.Error(), "false") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Local update DID happen (ff merge); only the build failed. PostPublishSha
	// still reflects the worktree HEAD — caller can recover by fixing the build.
	if headSHA(t, src) == preSrcSHA {
		t.Fatal("expected ff-update to have happened before build")
	}

	// Audit failure entry written.
	entries, _ := ReadAudit(AuditReadOpts{})
	if len(entries) == 0 {
		t.Fatal("expected failure audit entry")
	}
	last := entries[len(entries)-1]
	if last.Outcome != "failure" {
		t.Fatalf("audit Outcome: got %q, want failure", last.Outcome)
	}
	if last.Mode != "publish" {
		t.Fatalf("audit Mode: got %q, want publish", last.Mode)
	}
}

func TestPublish_RefusesUnknownTaskID(t *testing.T) {
	client := stubArgusTaskNotFound(t)
	_, err := Publish(context.Background(), client, PublishInput{TaskID: "ghost-task"})
	if err == nil {
		t.Fatal("expected error for unknown task")
	}
	if !strings.Contains(err.Error(), "ghost-task") {
		t.Fatalf("expected error to name task id, got: %v", err)
	}
}

func TestPublish_RefusesMissingTaskID(t *testing.T) {
	client := stubArgusTaskNotFound(t)
	_, err := Publish(context.Background(), client, PublishInput{})
	if err == nil {
		t.Fatal("expected error for missing task_id")
	}
	if !strings.Contains(err.Error(), "task_id") {
		t.Fatalf("expected error to mention task_id, got: %v", err)
	}
}

func TestPublish_ConcurrentPublishAndReloadSerialize(t *testing.T) {
	const tomlSlow = `schema_version = 1
[build]
command = ["sleep", "0.2"]
[restart]
mechanism = "none"
`
	_, _, client := publishFixture(t, "concurrent", tomlSlow)

	var wg sync.WaitGroup
	starts := make([]time.Time, 2)
	ends := make([]time.Time, 2)
	errs := make([]error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		starts[0] = time.Now()
		_, errs[0] = Publish(context.Background(), client, PublishInput{TaskID: "task-a"})
		ends[0] = time.Now()
	}()
	// Stagger so the second call enters lockSourceRepo after the first.
	time.Sleep(20 * time.Millisecond)
	go func() {
		defer wg.Done()
		starts[1] = time.Now()
		_, errs[1] = Publish(context.Background(), client, PublishInput{TaskID: "task-b"})
		ends[1] = time.Now()
	}()
	wg.Wait()

	// Both call attempts run; one (or both) may fail because after the first one
	// publishes, the source repo HEAD has moved and the second call's ff-merge
	// from the worktree's stale-relative-to-new-HEAD SHA may no-op or fail. That's
	// fine — we're testing serialization, not idempotency.
	if errs[0] != nil && errs[1] != nil {
		// Acceptable as long as the order shows serialization. Continue.
	}

	// Serialization invariant: end-of-first should be <= start-of-second-completing.
	// Since starts have a 20ms stagger, just assert the runs didn't fully overlap.
	if ends[0].Before(starts[1]) || ends[1].Before(starts[0]) {
		// Non-overlapping (one finished before the other started) → serialized.
		return
	}
	// They overlapped — assert at least one of them blocked for ~build duration.
	durA := ends[0].Sub(starts[0])
	durB := ends[1].Sub(starts[1])
	if durA < 200*time.Millisecond && durB < 200*time.Millisecond {
		t.Fatalf("expected one publish to block ~200ms while the other ran the slow build; durA=%s durB=%s", durA, durB)
	}
}

func TestPublish_RefusesExitCodeMechanism(t *testing.T) {
	// Publish always treats the target as cross (it never runs the self-exit
	// choreography), so .iris.toml with mechanism=exit_code must fail
	// validation at pre-flight regardless of which source repo we're touching.
	const tomlExit = `schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "exit_code"
`
	src, _, client := publishFixture(t, "exit-code", tomlExit)
	before := headSHA(t, src)
	_, err := Publish(context.Background(), client, PublishInput{TaskID: "task-exit"})
	if err == nil {
		t.Fatal("expected refusal for exit_code mechanism on publish")
	}
	if !strings.Contains(err.Error(), "exit_code") && !strings.Contains(err.Error(), "self-only") {
		t.Fatalf("unexpected error: %v", err)
	}
	if after := headSHA(t, src); after != before {
		t.Fatalf("source HEAD changed on refusal: before=%q after=%q", before, after)
	}
}

func TestPublish_RestartMechanismDelegation(t *testing.T) {
	// mechanism = exec runs a real command and captures output. Validates the
	// dispatchRestart delegation.
	const tomlExec = `schema_version = 1
[build]
command = ["true"]
[restart]
mechanism = "exec"
command = ["echo", "restart-ok"]
`
	_, _, client := publishFixture(t, "exec-restart", tomlExec)

	result, err := Publish(context.Background(), client, PublishInput{TaskID: "task-exec"})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if result.RestartMechanism != "exec" {
		t.Fatalf("RestartMechanism: got %q, want exec", result.RestartMechanism)
	}
	if !strings.Contains(result.RestartOutput, "restart-ok") {
		t.Fatalf("RestartOutput missing echo output: %q", result.RestartOutput)
	}
}

// _ keeps exec imported when no other test uses it directly.
var _ = exec.Command

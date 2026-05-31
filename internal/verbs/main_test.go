package verbs

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain installs a throwaway, hermetic global git config for the whole
// verbs test binary and disables git's fsync.
//
// The suite forks git thousands of times: every fixture builds a repo from
// scratch (init/clone/commit/push/worktree) and the verbs under test run
// real git operations (ff-merge, push, cherry-pick, worktree publish). Git
// fsyncs each loose object, pack, and ref update by default, which makes the
// suite disk-bound — and once tests run in parallel, that fsync traffic, not
// CPU, dominates wall-clock. Disabling fsync is safe here because every repo
// lives under t.TempDir() and is thrown away at the end of the test; none of
// it needs to survive a crash.
//
// Pinning GIT_CONFIG_GLOBAL/NOSYSTEM also makes the suite hermetic: it no
// longer reads the developer's ~/.gitconfig or the system config, so a
// machine-specific setting can't change a test outcome.
//
// This is set once, before m.Run() and therefore before any test or
// t.Parallel() executes, so it introduces no shared-state race — unlike a
// per-test t.Setenv, which would panic under t.Parallel().
func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	dir, err := os.MkdirTemp("", "iris-verbs-gitconfig")
	if err != nil {
		panic("verbs testmain: mkdtemp: " + err.Error())
	}
	defer os.RemoveAll(dir)

	cfg := filepath.Join(dir, "gitconfig")
	const body = "[user]\n" +
		"\tname = iris-test\n" +
		"\temail = iris-test@example.com\n" +
		"[init]\n" +
		"\tdefaultBranch = main\n" +
		"[core]\n" +
		"\tfsync = none\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		panic("verbs testmain: write gitconfig: " + err.Error())
	}

	os.Setenv("GIT_CONFIG_GLOBAL", cfg)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	return m.Run()
}

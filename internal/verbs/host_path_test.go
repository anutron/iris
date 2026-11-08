package verbs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/config"
)

// Every fixture lives under TempDir. In particular, the restart is a fake
// executable, never launchctl or the operator's argus binary.
func TestHostCommandsFindGoTools(t *testing.T) {
	src, wt := setupRepoWithWorktree(t, "host-go-path")
	client := stubArgus(t, src, wt)
	home := t.TempDir()
	bin := filepath.Join(home, "go", "bin")
	writeExecScript(t, filepath.Join(bin, "gotestsum"), "#!/bin/sh\nprintf 'go-tool:%s' \"$IRIS_PATH_MARKER\"\n")
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	t.Setenv("IRIS_PATH_MARKER", "inherited")
	ctx := context.Background()
	for _, name := range []string{"iris-build", "iris-check"} {
		writeExecScript(t, filepath.Join(wt, "script", name), "#!/bin/sh\ngotestsum\n")
	}
	writeExecScript(t, filepath.Join(bin, "gh"), "#!/bin/sh\nprintf '{\"url\":\"https://github.com/test/repo/pull/1\",\"title\":\"'\ngotestsum\nprintf '\"}'")
	for _, dir := range []string{src, wt} {
		if err := os.WriteFile(filepath.Join(dir, ".iris.toml"), []byte("schema_version = 1\n[post_merge]\ncommand = [\"/bin/sh\", \"-c\", \"gotestsum\"]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	resolved := &ResolvedRepo{SourceRepo: src, Task: &argus.Task{ID: "task-path"}}
	cases := []struct {
		name string
		run  func() (string, error)
		want string
	}{
		{"configured build", func() (string, error) {
			return runBuildBlock(ctx, src, config.BuildBlock{Command: []string{"gotestsum"}, Env: map[string]string{"IRIS_PATH_MARKER": "overlay", "PATH": "/usr/bin:/bin"}}, config.SecretsBlock{})
		}, "go-tool:overlay"},
		{"hook", func() (string, error) {
			return runHook(ctx, src, config.HookBlock{Command: []string{"gotestsum"}}, 10)
		}, "go-tool:inherited"},
		{"exec restart", func() (string, error) {
			out, _, err := dispatchRestart(ctx, config.RestartBlock{Mechanism: config.MechanismExec, Command: []string{"gotestsum"}}, false, config.SecretsBlock{})
			return out, err
		}, "go-tool:inherited"},
		{"worktree build", func() (string, error) {
			result, err := RunBuild(ctx, client, "task-path", RunBuildOptions{})
			if result == nil {
				return "", err
			}
			return result.Output, err
		}, "go-tool:inherited"},
		{"worktree check", func() (string, error) {
			result, err := RunChecks(ctx, client, "task-path", RunChecksOptions{Check: "test"})
			if result == nil {
				return "", err
			}
			return result.Output, err
		}, "go-tool:inherited"},
		{"git descendant", func() (string, error) {
			return runGit(ctx, src, "-c", "alias.iris-path=!gotestsum", "iris-path")
		}, "go-tool:inherited"},
		{"gh executable", func() (string, error) {
			id, err := lookupPR(ctx, src, 1)
			if id == nil {
				return "", err
			}
			return id.Title, err
		}, "go-tool:inherited"},
		{"default branch post merge hook", func() (string, error) {
			result, err := runPostMergeHook(ctx, resolved, "main", "abc")
			if result == nil {
				return "", err
			}
			return result.Stdout, err
		}, "go-tool:inherited"},
		{"target branch post merge hook", func() (string, error) {
			result, err := runMergeToBranchPostMergeHook(ctx, resolved, wt, "main", "feature", "abc")
			if result == nil {
				return "", err
			}
			return result.Stdout, err
		}, "go-tool:inherited"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tc.run()
			if err != nil || strings.TrimSpace(out) != tc.want {
				t.Fatalf("got output %q, error %v; want %q", out, err, tc.want)
			}
		})
	}
	// Prove the fixture didn't silently modify the parent process PATH.
	if os.Getenv("PATH") != "/usr/bin:/bin" {
		t.Fatal("parent PATH changed")
	}
}

func TestHostCommandDoesNotWaitForGoDiscovery(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "launchd-bin")
	pidFile := filepath.Join(home, "fake-go.pid")
	writeExecScript(t, filepath.Join(path, "go"), "#!/bin/sh\nprintf '%s' $$ > \"$IRIS_DISCOVERY_PID\"\nexec /bin/sleep 30\n")
	writeExecScript(t, filepath.Join(home, "go", "bin", "gotestsum"), "#!/bin/sh\nprintf immediate-fallback\n")
	t.Setenv("HOME", home)
	t.Setenv("PATH", path+":/usr/bin:/bin")
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	t.Setenv("IRIS_DISCOVERY_PID", pidFile)
	start := time.Now()
	out, err := runArgv(context.Background(), 2*time.Second, nil, "gotestsum")
	elapsed := time.Since(start)
	// The PID comes exclusively from the temporary fake Go executable. Terminate
	// it and wait for reaping so this regression never leaves a process behind.
	pid := readPID(t, pidFile)
	t.Cleanup(func() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		requireGone(t, pid)
	})
	if err != nil || out != "immediate-fallback" {
		t.Fatalf("cold verb: output %q, error %v, elapsed %s", out, err, elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("cold verb waited for Go discovery: %s", elapsed)
	}
	t.Logf("cold verb completed in %s while fake Go discovery hung", elapsed)
}

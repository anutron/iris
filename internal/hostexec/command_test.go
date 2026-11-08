package hostexec

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func script(t *testing.T, file, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestCommandContextGoDirectory(t *testing.T) {
	for _, tc := range []struct{ name, gobin, gopath, exportedBin, exportedPath, goBody string }{
		{name: "GOBIN", gobin: "custom bin", gopath: "unused"},
		{name: "GOPATH", gopath: "workspace"},
		{name: "GOPATH list", gopath: "workspace:second"},
		{name: "Go default", goBody: "printf '{}'"},
		{name: "Go absent"},
		{name: "Go fails", goBody: "exit 1"},
		{name: "Go invalid output", goBody: "printf 'invalid'"},
		{name: "exported GOBIN without Go", exportedBin: "exported bin"},
		{name: "exported GOPATH without Go", exportedPath: "exported workspace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, "launchd-bin")
			want := filepath.Join(home, "go", "bin")
			dirs := map[string]string{"GOBIN": "", "GOPATH": ""}
			if tc.gopath != "" {
				var paths []string
				for _, dir := range strings.Split(tc.gopath, ":") {
					paths = append(paths, filepath.Join(home, dir))
				}
				dirs["GOPATH"] = strings.Join(paths, string(os.PathListSeparator))
				want = filepath.Join(paths[0], "bin")
			}
			if tc.gobin != "" {
				dirs["GOBIN"] = filepath.Join(home, tc.gobin)
				want = dirs["GOBIN"]
			}
			body := tc.goBody
			if tc.gobin != "" || tc.gopath != "" {
				body = "test \"$*\" = 'env -json GOBIN GOPATH' || exit 2\n" +
					"test \"$GOTOOLCHAIN:$GOWORK\" = 'local:off' && test \"$PWD\" -ef \"$HOME\" || exit 3\n" +
					"printf '%s' \"$FAKE_GO_ENV\""
			}
			if body != "" {
				script(t, filepath.Join(path, "go"), body)
			}
			encoded, _ := json.Marshal(dirs)
			env := []string{"HOME=" + home, "PATH=" + path, "FAKE_GO_ENV=" + string(encoded), "MARKER=preserved"}
			if tc.exportedBin != "" {
				want = filepath.Join(home, tc.exportedBin)
				env = append(env, "GOBIN="+want)
			}
			if tc.exportedPath != "" {
				gp := filepath.Join(home, tc.exportedPath)
				want = filepath.Join(gp, "bin")
				env = append(env, "GOPATH="+gp)
			}
			script(t, filepath.Join(want, "gotestsum"), "printf '%s|%s' \"$MARKER\" \"$1\"")
			original := strings.Join(env, "\n")
			// Explicitly await refinement here; cold-call fallback has its own
			// hung-discovery regression below.
			<-startDiscovery(env).done
			for _, command := range [][]string{{"gotestsum", "argument with spaces"}, {"/bin/sh", "-c", "gotestsum 'argument with spaces'"}} {
				cmd := CommandContext(context.Background(), env, command[0], command[1:]...)
				out, err := cmd.CombinedOutput()
				if err != nil || string(out) != "preserved|argument with spaces" {
					t.Fatalf("%v: %q, %v", command, out, err)
				}
				if got := value(cmd.Env, "PATH"); got != path+string(os.PathListSeparator)+want {
					t.Fatalf("PATH=%q", got)
				}
			}
			if strings.Join(env, "\n") != original {
				t.Fatal("caller environment mutated")
			}
		})
	}
}

func TestCommandContextPreservesPrecedenceAndDeduplicates(t *testing.T) {
	home := t.TempDir()
	first, bin := filepath.Join(home, "first"), filepath.Join(home, "go", "bin")
	script(t, filepath.Join(first, "argus"), "printf first")
	script(t, filepath.Join(bin, "argus"), "printf go-bin")
	for _, path := range []string{first, first + ":" + bin, first + ":" + bin + "/"} {
		cmd := CommandContext(context.Background(), []string{"HOME=" + home, "PATH=" + path}, "argus")
		out, err := cmd.Output()
		if err != nil || string(out) != "first" {
			t.Fatalf("precedence: %q, %v", out, err)
		}
		if strings.Count(value(cmd.Env, "PATH"), bin) != 1 {
			t.Fatalf("duplicate PATH: %v", cmd.Env)
		}
	}
}

func TestCommandContextRejectsRelativeSearchPath(t *testing.T) {
	// Use a relative entry for a tool that exists, without changing process cwd.
	home := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(wd, home)
	if err != nil {
		t.Fatal(err)
	}
	script(t, filepath.Join(home, "tool"), "exit 0")
	cmd := CommandContext(context.Background(), []string{"HOME=" + home, "PATH=" + rel}, "tool")
	if !errors.Is(cmd.Run(), exec.ErrDot) {
		t.Fatalf("expected ErrDot, got %v", cmd.Err)
	}
	cmd = CommandContext(context.Background(), []string{"HOME=" + home, "PATH=" + rel}, "./tool")
	cmd.Dir = home
	if err := cmd.Run(); err != nil {
		t.Fatalf("explicit relative path: %v", err)
	}
}

func TestCommandContextCachesConcurrentDiscovery(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "bin")
	count := filepath.Join(home, "calls")
	script(t, filepath.Join(path, "go"), "printf x >> \"$CALLS\"\nprintf '{}'")
	env := []string{"HOME=" + home, "PATH=" + path, "CALLS=" + count}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); CommandContext(context.Background(), env, "/bin/true") }()
	}
	wg.Wait()
	<-startDiscovery(env).done
	data, err := os.ReadFile(count)
	if err != nil || string(data) != "x" {
		t.Fatalf("discovery calls: %q, %v", data, err)
	}
}

func TestCommandContextBoundsGoDiscovery(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "bin")
	script(t, filepath.Join(path, "go"), "exec /bin/sleep 30")
	script(t, filepath.Join(home, "go", "bin", "tool"), "printf fallback")
	start := time.Now()
	env := []string{"HOME=" + home, "PATH=" + path}
	t.Cleanup(func() { <-startDiscovery(env).done })
	cmd := CommandContext(context.Background(), env, "tool")
	out, err := cmd.Output()
	if err != nil || string(out) != "fallback" {
		t.Fatalf("fallback: %q, %v", out, err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("cold command waited for discovery: %s", elapsed)
	} else {
		t.Logf("cold fallback command completed in %s while Go discovery hung", elapsed)
	}
}

func TestCommandContextMissingExecutable(t *testing.T) {
	cmd := CommandContext(context.Background(), []string{"HOME=" + t.TempDir(), "PATH=/nonexistent"}, "missing-iris-test-tool")
	if !errors.Is(cmd.Run(), exec.ErrNotFound) {
		t.Fatalf("expected ErrNotFound: %v", cmd.Err)
	}
}

func TestCommandContextCancellationDuringDiscovery(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "bin")
	script(t, filepath.Join(path, "go"), "exec /bin/sleep 30")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env := []string{"HOME=" + home, "PATH=" + path}
	// Drain the independently bounded query before TempDir removes its fixture,
	// so the test cannot leave its fake Go process running after package exit.
	t.Cleanup(func() { <-startDiscovery(env).done })
	start := time.Now()
	cmd := CommandContext(ctx, env, "/bin/true")
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("canceled caller waited %s for discovery", elapsed)
	}
	if err := cmd.Run(); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected command deadline, got %v", err)
	}
}

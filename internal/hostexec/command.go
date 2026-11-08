// Package hostexec builds the environment shared by iris host commands.
package hostexec

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Discovery is independent of command arguments, repository modules and secrets.
// Persisted go env changes take effect in a new iris process.
var goDirs sync.Map

// Allow a cold Go process to start on a busy host. Individual callers can stop
// waiting sooner through their own contexts; successful discovery is cached.
const discoveryTimeout = 10 * time.Second

type discovery struct {
	once sync.Once
	done chan struct{}
	bin  string
}

// CommandContext uses one augmented PATH for executable lookup and descendants.
// A nil env inherits the process environment; otherwise env is the complete
// overlaid child environment. It never mutates the process environment.
func CommandContext(ctx context.Context, env []string, name string, args ...string) *exec.Cmd {
	if env == nil {
		env = os.Environ()
	}
	env = append([]string(nil), env...)
	bin := goBin(env)
	path := value(env, "PATH")
	if bin != "" {
		found := false
		for _, dir := range filepath.SplitList(path) {
			if filepath.Clean(dir) == bin {
				found = true
				break
			}
		}
		if !found {
			if path != "" {
				path += string(os.PathListSeparator)
			}
			path += bin
		}
	}
	env = withValue(env, "PATH", path)
	resolved, err := lookPath(name, path)
	cmd := exec.CommandContext(ctx, resolved, args...)
	cmd.Args[0] = name
	cmd.Env = env
	if err != nil {
		cmd.Err = err
	}
	return cmd
}

func value(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], key+"="); ok {
			return v
		}
	}
	return ""
}

func withValue(env []string, key, val string) []string {
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, key+"=") {
			out = append(out, entry)
		}
	}
	return append(out, key+"="+val)
}

// Match exec.LookPath's executable and ErrDot rules without changing os.Getenv.
func lookPath(name, path string) (string, error) {
	if strings.ContainsRune(name, '/') {
		return name, nil
	}
	for _, dir := range filepath.SplitList(path) {
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		if !filepath.IsAbs(candidate) {
			return name, &exec.Error{Name: name, Err: exec.ErrDot}
		}
		return candidate, nil
	}
	return name, &exec.Error{Name: name, Err: exec.ErrNotFound}
}

// Warm starts discovery for the daemon environment without waiting for Go.
func Warm() { startDiscovery(os.Environ()) }

func goBin(env []string) string {
	d := startDiscovery(env)
	select {
	case <-d.done:
		return d.bin
	default:
		return fallbackGoBin(env)
	}
}

func startDiscovery(env []string) *discovery {
	// Cache only Go-discovery inputs, never unrelated variables or secrets.
	var key strings.Builder
	for _, name := range []string{"PATH", "HOME", "GOBIN", "GOPATH", "GOENV", "GOROOT"} {
		key.WriteString(value(env, name))
		key.WriteByte(0)
	}
	entry, _ := goDirs.LoadOrStore(key.String(), &discovery{done: make(chan struct{})})
	d := entry.(*discovery)
	d.once.Do(func() {
		go func() {
			d.bin = discoverGoBin(env)
			close(d.done)
		}()
	})
	return d
}

func discoverGoBin(env []string) string {
	home := value(env, "HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	gobin, gopath := value(env, "GOBIN"), value(env, "GOPATH")
	if goPath, err := lookPath("go", value(env, "PATH")); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), discoveryTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, goPath, "env", "-json", "GOBIN", "GOPATH")
		// Avoid repository-specific toolchain downloads or workspace discovery.
		cmd.Dir = home
		cmd.Env = withValue(withValue(env, "GOTOOLCHAIN", "local"), "GOWORK", "off")
		cmd.WaitDelay = 100 * time.Millisecond
		var dirs struct{ GOBIN, GOPATH string }
		if out, err := cmd.Output(); err == nil && json.Unmarshal(out, &dirs) == nil {
			gobin, gopath = dirs.GOBIN, dirs.GOPATH
		}
	}
	return installDir(home, gobin, gopath)
}

func fallbackGoBin(env []string) string {
	home := value(env, "HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return installDir(home, value(env, "GOBIN"), value(env, "GOPATH"))
}

func installDir(home, gobin, gopath string) string {
	if filepath.IsAbs(gobin) {
		return filepath.Clean(gobin)
	}
	if paths := filepath.SplitList(gopath); len(paths) > 0 && filepath.IsAbs(paths[0]) {
		return filepath.Join(paths[0], "bin")
	}
	if filepath.IsAbs(home) {
		return filepath.Join(home, "go", "bin")
	}
	return ""
}

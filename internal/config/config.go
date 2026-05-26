// Package config holds iris's runtime configuration with working defaults.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config bundles iris's runtime configuration. All fields have working
// defaults; the daemon does not require a user-supplied config file.
type Config struct {
	// StateDir is where iris keeps its token, PID, launchd log, and the
	// stable symlink the LaunchAgent points at. Default: ~/.iris
	StateDir string

	// ListenAddr is the address the MCP callback HTTP listener binds to.
	// Default: 127.0.0.1:0 (any free port; callback URL derived from the
	// actual bound address).
	ListenAddr string

	// MCPHeartbeat is how often iris re-POSTs its tool registrations to
	// argus to stay within argus's idle sweep window. Default: 5m.
	MCPHeartbeat time.Duration

	// ArgusSocketPath is the unix-domain socket exposed by the argus
	// daemon for the Daemon.* RPC family (Ports, Ping). Iris queries it
	// on startup to discover argus's dynamic REST port.
	// Default: ~/.argus/daemon.sock
	ArgusSocketPath string
}

// Default returns a Config populated with the v1 defaults.
func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		StateDir:        filepath.Join(home, ".iris"),
		ListenAddr:      "127.0.0.1:0",
		MCPHeartbeat:    5 * time.Minute,
		ArgusSocketPath: filepath.Join(home, ".argus", "daemon.sock"),
	}
}

// TokenPath returns the path to the scope-token file iris reads on startup.
func (c *Config) TokenPath() string { return filepath.Join(c.StateDir, "api-token") }

// PIDPath returns the path to iris's PID file.
func (c *Config) PIDPath() string { return filepath.Join(c.StateDir, "iris.pid") }

// LogPath returns the path to iris's launchd-captured log file.
func (c *Config) LogPath() string { return filepath.Join(c.StateDir, "launchd.log") }

// EnsureStateDir creates the configured StateDir with 0o700 permissions
// (token-grade) if missing.
func (c *Config) EnsureStateDir() error { return os.MkdirAll(c.StateDir, 0o700) }

// LoadToken reads the scope token from c.TokenPath(). Missing or empty
// files produce an actionable error.
func (c *Config) LoadToken() (string, error) {
	path := c.TokenPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf(
				"iris: scope token file %s not found\n\n"+
					"Run: argus token mint --scope iris > %s\n"+
					"     chmod 600 %s\n",
				path, path, path,
			)
		}
		return "", fmt.Errorf("iris: read token file %s: %w", path, err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf(
			"iris: scope token file %s is empty\n\n"+
				"Run: argus token mint --scope iris > %s\n",
			path, path,
		)
	}
	return token, nil
}

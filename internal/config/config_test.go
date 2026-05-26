package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Host-bridge scenario: "Daemon refuses to start without a scope token"
// (missing-file branch).
func TestLoadToken_MissingFile(t *testing.T) {
	cfg := &Config{StateDir: filepath.Join(t.TempDir(), "iris-empty")}
	_, err := cfg.LoadToken()
	if err == nil {
		t.Fatal("expected error for missing token file, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "argus token mint --scope iris") {
		t.Fatalf("error must name the mint command, got: %v", err)
	}
	if !strings.Contains(msg, cfg.TokenPath()) {
		t.Fatalf("error must name the token path, got: %v", err)
	}
}

// Host-bridge scenario: "Daemon refuses to start without a scope token"
// (empty-file branch — whitespace-only counts as empty after TrimSpace).
func TestLoadToken_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{StateDir: dir}
	if err := cfg.EnsureStateDir(); err != nil {
		t.Fatalf("EnsureStateDir: %v", err)
	}
	if err := os.WriteFile(cfg.TokenPath(), []byte("   \n  \t  "), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	_, err := cfg.LoadToken()
	if err == nil {
		t.Fatal("expected error for whitespace-only token file, got nil")
	}
	if !strings.Contains(err.Error(), "argus token mint --scope iris") {
		t.Fatalf("error must name the mint command, got: %v", err)
	}
}

// Sanity: a token file with a real token loads cleanly.
func TestLoadToken_HappyPath(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{StateDir: dir}
	if err := cfg.EnsureStateDir(); err != nil {
		t.Fatalf("EnsureStateDir: %v", err)
	}
	const tok = "abc123"
	if err := os.WriteFile(cfg.TokenPath(), []byte("  "+tok+"\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	got, err := cfg.LoadToken()
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if got != tok {
		t.Fatalf("LoadToken: got %q, want %q", got, tok)
	}
}

func TestEnsureStateDir_Mode0700(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "iris-fresh")
	cfg := &Config{StateDir: dir}
	if err := cfg.EnsureStateDir(); err != nil {
		t.Fatalf("EnsureStateDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o700 {
		t.Fatalf("state dir mode: got %o, want 0700", mode)
	}
}

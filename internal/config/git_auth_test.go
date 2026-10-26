package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGitAuth_LocalWithoutSharedAndMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, IrisLocalTomlFilename)
	if err := os.WriteFile(path, []byte("git_auth_token_source = 'op://test/token/value'\n[secrets.op]\nbootstrap_source = 'env://BOOTSTRAP'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, sc, err := LoadGitAuth(dir)
	if err != nil || source != "op://test/token/value" || sc.Op.BootstrapSource != "env://BOOTSTRAP" {
		t.Fatalf("load: %s %v", source, err)
	}
	if err := os.WriteFile(path, []byte("git_auth_token_source = ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadGitAuth(dir); err == nil {
		t.Fatal("malformed config failed open")
	}
}

func TestLoadGitAuth_LocalOverride(t *testing.T) {
	dir := t.TempDir()
	for file, body := range map[string]string{IrisTomlFilename: "git_auth_token_source = 'env://SHARED'\n", IrisLocalTomlFilename: "git_auth_token_source = ''\n"} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	source, _, err := LoadGitAuth(dir)
	if err != nil || source != "" {
		t.Fatal("explicit local opt-out ignored")
	}
	if kind, ok := FieldKind("git_auth_token_source"); !ok || kind != FieldKindLocal {
		t.Fatal("auth setting not local")
	}
}

func TestLoadGitAuth_RejectsRecursiveBootstrap(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, IrisLocalTomlFilename), []byte("git_auth_token_source = 'op://test/token/value'\n[secrets.op]\nbootstrap_source = 'op://recursive/bootstrap/value'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadGitAuth(dir); err == nil {
		t.Fatal("recursive bootstrap accepted")
	}
}

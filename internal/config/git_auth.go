package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// LoadGitAuth reads only transfer authentication and resolver settings. A
// shared build/restart declaration is optional for git-only repos. Local
// fields override shared ones, including an explicitly empty opt-out.
// Unlike the build runners' lenient secret peek, malformed/unreadable files
// fail closed. Parser errors and file contents are never returned.
func LoadGitAuth(repoRoot string) (string, SecretsBlock, error) {
	var source string
	var sc SecretsBlock
	for _, file := range []string{IrisTomlFilename, IrisLocalTomlFilename} {
		data, err := os.ReadFile(filepath.Join(repoRoot, file))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", sc, fmt.Errorf("cannot read %s for git authentication", file)
		}
		var doc struct {
			TokenSource string       `toml:"git_auth_token_source"`
			Secrets     SecretsBlock `toml:"secrets"`
		}
		meta, err := toml.Decode(string(data), &doc)
		if err != nil {
			return "", sc, fmt.Errorf("cannot parse %s for git authentication", file)
		}
		if meta.IsDefined("git_auth_token_source") {
			source = doc.TokenSource
		}
		if meta.IsDefined("secrets") {
			sc = doc.Secrets
		}
	}
	if !ValidGitAuthTokenSource(source) {
		return "", sc, fmt.Errorf("invalid git_auth_token_source descriptor")
	}
	if source != "" && len(sc.validate()) != 0 {
		return "", sc, fmt.Errorf("invalid secrets.op bootstrap configuration for git authentication")
	}
	return source, sc, nil
}

// ValidGitAuthTokenSource accepts descriptors, never an inline token value.
func ValidGitAuthTokenSource(source string) bool {
	if source == "" {
		return true
	}
	if strings.ContainsAny(source, "\r\n\x00") {
		return false
	}
	for _, scheme := range []string{"op://", "env://", "keychain://"} {
		if strings.HasPrefix(source, scheme) {
			return len(source) > len(scheme)
		}
	}
	return false
}

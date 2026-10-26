package verbs

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/anutron/iris/internal/config"
)

type tokenResolver func(context.Context, config.SecretsBlock, string) (string, bool)
type transferExecutor func(*exec.Cmd) ([]byte, error)

// The helper code is static: the credential itself is never an argument.
// Git invokes helpers with get/store/erase; only get emits credentials.
const gitTokenHelper = `!f() { if test "$1" = get; then printf 'username=x-access-token\npassword=%s\n' "$IRIS_GIT_AUTH_TOKEN"; fi; }; f`

var githubRepoPath = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func githubHTTPSRemote(raw string) (string, error) {
	var path string
	if strings.HasPrefix(raw, "git@github.com:") {
		path = strings.TrimPrefix(raw, "git@github.com:")
	} else {
		u, err := url.Parse(raw)
		if err != nil || u.Host != "github.com" || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
			return "", fmt.Errorf("requires a GitHub remote without embedded credentials")
		}
		if u.Scheme == "ssh" {
			if u.User != nil && u.User.String() != "git" {
				return "", fmt.Errorf("requires a GitHub SSH remote with user git")
			}
		} else if u.Scheme != "https" || u.User != nil {
			return "", fmt.Errorf("requires a GitHub HTTPS or SSH remote without embedded credentials")
		}
		path = strings.TrimPrefix(u.Path, "/")
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	if !githubRepoPath.MatchString(path) {
		return "", fmt.Errorf("requires a GitHub owner/repo remote")
	}
	for _, part := range strings.Split(path, "/") {
		if part == "." || part == ".." {
			return "", fmt.Errorf("invalid GitHub repository path")
		}
	}
	return "https://github.com/" + path + ".git", nil
}

// gitTransferCommand confines credentials to one command's environment.
// The returned redactor must be applied before capturing any output/error.
func gitTransferCommand(ctx context.Context, dir string, args []string, resolve tokenResolver) (*exec.Cmd, func(string) string, error) {
	identity := func(s string) string { return s }
	source, sc, err := config.LoadGitAuth(dir)
	fail := func(err error) (*exec.Cmd, func(string) string, error) {
		op := ""
		if len(args) > 0 {
			op = args[0]
		}
		return nil, identity, &GitTransferError{Op: op, Reason: GitTransferAuthFailure, Err: err}
	}
	if err != nil {
		return fail(err)
	}
	if source == "" {
		return exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...), identity, nil
	}
	if len(args) < 2 || (args[0] != "push" && args[0] != "fetch") {
		return fail(fmt.Errorf("unsupported token-auth transfer"))
	}
	remote := args[1]
	// Read raw config, not get-url's globally rewritten URL. Exact per-command
	// rewrite rules take precedence over broad inherited insteadOf rules.
	raw, err := runGit(ctx, dir, "config", "--get-all", "remote."+remote+".url")
	if err != nil {
		return fail(fmt.Errorf("cannot read configured git remote for token authentication"))
	}
	urls := strings.Split(strings.TrimSpace(raw), "\n")
	if len(urls) != 1 {
		return fail(fmt.Errorf("token authentication requires one remote URL"))
	}
	https, err := githubHTTPSRemote(urls[0])
	if err != nil {
		return fail(err)
	}
	if args[0] == "push" {
		pushRaw, pushErr := runGit(ctx, dir, "config", "--get-all", "remote."+remote+".pushurl")
		var exitErr *exec.ExitError
		if pushErr != nil && !(errors.As(pushErr, &exitErr) && exitErr.ExitCode() == 1) {
			return fail(fmt.Errorf("cannot read configured push URL for token authentication"))
		}
		if strings.TrimSpace(pushRaw) != "" {
			pushURLs := strings.Split(strings.TrimSpace(pushRaw), "\n")
			if len(pushURLs) != 1 {
				return fail(fmt.Errorf("token authentication requires one push URL"))
			}
			pushHTTPS, err := githubHTTPSRemote(pushURLs[0])
			if err != nil || pushHTTPS != https {
				return fail(fmt.Errorf("token authentication requires fetch and push URLs for the same GitHub repository"))
			}
			urls = append(urls, pushURLs[0])
		}
	}
	token, ok := resolve(ctx, sc, source)
	if !ok || token == "" || strings.ContainsAny(token, "\r\n\x00") {
		return fail(fmt.Errorf("cannot resolve usable git authentication token from %s", source))
	}
	full := []string{"-C", dir, "-c", "credential.helper=", "-c", "credential.helper=" + gitTokenHelper, "-c", "http.followRedirects=false"}
	for _, rawURL := range urls {
		full = append(full, "-c", "url."+https+".insteadOf="+rawURL, "-c", "url."+https+".pushInsteadOf="+rawURL)
	}
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	// Filter inherited copies so no stale variable overrides the selected
	// credential or re-enables a prompt. Disable tracing that could capture
	// the helper's credential-protocol traffic.
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if name == "IRIS_GIT_AUTH_TOKEN" || name == "GIT_TERMINAL_PROMPT" || name == "GIT_ASKPASS" || name == "SSH_ASKPASS" || strings.HasPrefix(name, "GIT_TRACE") || name == "GIT_CURL_VERBOSE" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "IRIS_GIT_AUTH_TOKEN="+token, "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=false", "SSH_ASKPASS=false")
	redactor := strings.NewReplacer(
		base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token)), "[REDACTED]",
		url.QueryEscape(token), "[REDACTED]",
		token, "[REDACTED]",
	)
	redact := redactor.Replace
	return cmd, redact, nil
}

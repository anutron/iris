package verbs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anutron/iris/internal/config"
)

func authFixture(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	g := gitRunner(t)
	g("", "init", dir)
	g(dir, "remote", "add", "origin", "git@github.com:acme/repo.git")
	if source != "" {
		if err := os.WriteFile(filepath.Join(dir, ".iris.local.toml"), []byte("git_auth_token_source = \""+source+"\"\n[secrets.op]\nbootstrap_source = 'env://TEST_BOOTSTRAP'\nbootstrap_target = 'OP_SERVICE_ACCOUNT_TOKEN'\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestGitTransferAuth_HTTPSAndRedaction(t *testing.T) {
	var logs bytes.Buffer
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prior) })
	const fakeToken = "FAKE_TEST_CREDENTIAL_DO_NOT_USE"
	for _, op := range []string{"push", "fetch"} {
		t.Run(op, func(t *testing.T) {
			dir := authFixture(t, "op://test/item/token")
			called := false
			resolve := func(ctx context.Context, sc config.SecretsBlock, source string) (string, bool) {
				if source != "op://test/item/token" || sc.Op.BootstrapSource != "env://TEST_BOOTSTRAP" {
					t.Fatal("wrong resolver input")
				}
				return fakeToken, true
			}
			execute := func(cmd *exec.Cmd) ([]byte, error) {
				called = true
				args := strings.Join(cmd.Args, " ")
				if strings.Contains(args, fakeToken) || !strings.Contains(args, "https://github.com/acme/repo.git.insteadOf=git@github.com:acme/repo.git") {
					t.Fatalf("unsafe or missing HTTPS argv: %s", args)
				}
				for _, want := range []string{"IRIS_GIT_AUTH_TOKEN=" + fakeToken, "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=false"} {
					found := false
					for _, env := range cmd.Env {
						if env == want {
							found = true
						}
					}
					if !found {
						t.Fatalf("missing environment name %s", strings.SplitN(want, "=", 2)[0])
					}
				}
				if !strings.Contains(args, "credential.helper=") || !strings.Contains(args, "$IRIS_GIT_AUTH_TOKEN") {
					t.Fatal("helper does not read environment")
				}
				return []byte("fatal: Authentication failed: " + fakeToken), errors.New("exit status 128: " + fakeToken)
			}
			out, err := runGitTransferWith(context.Background(), dir, time.Second, resolve, execute, op, "origin")
			if !called || err == nil || !strings.Contains(err.Error(), "[auth_failure]") {
				t.Fatalf("missing classified auth failure: %v", err)
			}
			if strings.Contains(out, fakeToken) || strings.Contains(err.Error(), fakeToken) {
				t.Fatal("credential leaked in output/error")
			}
			if strings.Contains(logs.String(), fakeToken) {
				t.Fatal("credential leaked in logs")
			}
			if got := gitRunner(t)(dir, "remote", "get-url", "origin"); strings.TrimSpace(got) != "git@github.com:acme/repo.git" {
				t.Fatal("stored remote changed")
			}
		})
	}
}

func TestGitTransferAuth_LocalConfigSourceValidation(t *testing.T) {
	for _, val := range []any{"op://test/item/token", "", 42, "inline-value", "op://bad\nvalue"} {
		err := validateLocalConfigValues(context.Background(), t.TempDir(), map[string]any{"git_auth_token_source": val})
		wantValid := val == "op://test/item/token" || val == ""
		if (err == nil) != wantValid {
			t.Fatalf("source validator accepted/rejected wrong input type %T", val)
		}
	}
}

func TestGitTransferAuth_ResolutionDeadlineIsTimeout(t *testing.T) {
	dir := authFixture(t, "op://test/item/token")
	_, err := runGitTransferWith(context.Background(), dir, 100*time.Millisecond,
		func(ctx context.Context, _ config.SecretsBlock, _ string) (string, bool) {
			<-ctx.Done()
			return "", false
		},
		func(*exec.Cmd) ([]byte, error) { t.Fatal("transfer executed after timeout"); return nil, nil }, "fetch", "origin")
	if !IsGitTransferTimeout(err) {
		t.Fatalf("resolution timeout misclassified: %v", err)
	}
}

func TestGitTransferAuth_FailClosed(t *testing.T) {
	for _, value := range []string{"", "bad\ncredential"} {
		dir := authFixture(t, "op://test/item/missing")
		out, err := runGitTransferWith(context.Background(), dir, time.Second,
			func(context.Context, config.SecretsBlock, string) (string, bool) { return value, value != "" },
			func(*exec.Cmd) ([]byte, error) {
				t.Fatal("transfer executed without usable credential")
				return nil, nil
			}, "push", "origin")
		if out != "" || err == nil || !strings.Contains(err.Error(), "[auth_failure]") || !strings.Contains(err.Error(), "op://test/item/missing") {
			t.Fatalf("not fail closed: %q %v", out, err)
		}
	}
}

func TestGitTransferAuth_OptOutUnchanged(t *testing.T) {
	dir := authFixture(t, "")
	_, err := runGitTransferWith(context.Background(), dir, time.Second,
		func(context.Context, config.SecretsBlock, string) (string, bool) {
			t.Fatal("resolved secret on opt-out")
			return "", false
		},
		func(cmd *exec.Cmd) ([]byte, error) {
			if cmd.Env != nil || strings.Join(cmd.Args[1:], " ") != "-C "+dir+" fetch origin" {
				t.Fatalf("changed opt-out command: %v", cmd.Args)
			}
			return nil, nil
		}, "fetch", "origin")
	if err != nil {
		t.Fatal(err)
	}
}

func TestGitTransferAuth_RemoteSafety(t *testing.T) {
	for _, remoteURL := range []string{"git@example.com:acme/repo.git", "https://github.com.evil.test/acme/repo", "https://user:password@github.com/acme/repo", "git@github.com:acme/repo.git/evil"} {
		dir := authFixture(t, "op://test/item/token")
		gitRunner(t)(dir, "remote", "set-url", "origin", remoteURL)
		_, err := runGitTransferWith(context.Background(), dir, time.Second,
			func(context.Context, config.SecretsBlock, string) (string, bool) {
				t.Fatal("resolved for unsafe remote")
				return "", false
			},
			func(*exec.Cmd) ([]byte, error) { t.Fatal("executed unsafe remote"); return nil, nil }, "push", "origin")
		if err == nil || !strings.Contains(err.Error(), "[auth_failure]") || strings.Contains(err.Error(), "password") {
			t.Fatalf("unsafe remote error: %v", err)
		}
	}
}

func TestGitTransferAuth_EffectiveRemoteAndPushURL(t *testing.T) {
	dir := authFixture(t, "env://TEST_TOKEN")
	g := gitRunner(t)
	g(dir, "remote", "add", "upstream", "ssh://git@github.com/other/project.git")
	resolve := func(context.Context, config.SecretsBlock, string) (string, bool) { return "FAKE", true }
	_, err := runGitTransferWith(context.Background(), dir, time.Second, resolve, func(cmd *exec.Cmd) ([]byte, error) {
		if !strings.Contains(strings.Join(cmd.Args, " "), "https://github.com/other/project.git.insteadOf=ssh://git@github.com/other/project.git") {
			t.Fatal("ignored effective remote")
		}
		return nil, nil
	}, "push", "upstream")
	if err != nil {
		t.Fatal(err)
	}
	g(dir, "config", "remote.origin.pushurl", "git@github.com:other/project.git")
	_, err = runGitTransferWith(context.Background(), dir, time.Second, resolve, func(*exec.Cmd) ([]byte, error) { t.Fatal("push destination mismatch ignored"); return nil, nil }, "push", "origin")
	if err == nil {
		t.Fatal("expected mismatched destination refusal")
	}
}

func TestGitTransferAuth_RealHelperAndURLRewrite(t *testing.T) {
	dir := authFixture(t, "op://test/item/token")
	// An inherited broad push rewrite must not steer the opted-in command
	// back to SSH. No network command is executed here.
	gitRunner(t)(dir, "config", "url.git@github.com:.pushInsteadOf", "git@github.com:")
	const fakeToken = "FAKE_HELPER_TEST_VALUE"
	resolve := func(context.Context, config.SecretsBlock, string) (string, bool) { return fakeToken, true }
	for _, op := range []string{"push", "fetch"} {
		cmd, _, err := gitTransferCommand(context.Background(), dir, []string{op, "origin"}, resolve)
		if err != nil {
			t.Fatal(err)
		}
		cmd.Args = append(cmd.Args[:len(cmd.Args)-2], "remote", "get-url")
		if op == "push" {
			cmd.Args = append(cmd.Args, "--push")
		}
		cmd.Args = append(cmd.Args, "origin")
		out, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "https://github.com/acme/repo.git" {
			t.Fatalf("rewrite failed (%s): %v", op, err)
		}
	}
	cmd, _, err := gitTransferCommand(context.Background(), dir, []string{"fetch", "origin"}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Args = append(cmd.Args[:len(cmd.Args)-2], "credential", "fill")
	cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v", err)
	}
	if !strings.Contains(string(out), "username=x-access-token\n") || !strings.Contains(string(out), "password="+fakeToken+"\n") {
		t.Fatal("helper did not supply credentials")
	}
}

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anutron/iris/internal/argus"
)

// Exercise the actual handlers and git subprocesses against a local bare
// remote. No host git configuration or interactive credentials are used.
func transferFixture(t *testing.T) (*argus.Client, string, string, string) {
	t.Helper()
	tmp := t.TempDir()
	bare, src, wt := filepath.Join(tmp, "remote.git"), filepath.Join(tmp, "src"), filepath.Join(tmp, "wt")
	g := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=iris-test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=iris-test", "GIT_COMMITTER_EMAIL=test@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	g("init", "--bare", "-b", "main", bare)
	g("clone", bare, src)
	g("-C", bare, "config", "core.hooksPath", filepath.Join(bare, "hooks"))
	g("-C", src, "config", "core.hooksPath", filepath.Join(src, ".git", "hooks"))
	g("-C", src, "config", "commit.gpgsign", "false")
	g("-C", src, "commit", "--allow-empty", "-m", "initial")
	g("-C", src, "push", "origin", "main")
	g("-C", src, "remote", "set-head", "origin", "main")
	g("-C", src, "worktree", "add", "-b", "argus/transfer", wt)
	g("-C", wt, "commit", "--allow-empty", "-m", "work")
	sha := g("-C", wt, "rev-parse", "HEAD")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/tasks/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"worktree_path": wt})
		case r.URL.Path == "/api/projects/full":
			_ = json.NewEncoder(w).Encode(map[string]any{"projects": []map[string]string{{"name": "test", "path": src}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return argus.New(srv.URL, "stub-token"), src, bare, sha
}

func TestTransferHandlers_FastInline(t *testing.T) {
	client, _, _, sha := transferFixture(t)
	runs := NewRunRegistry()
	for name, h := range map[string]Handler{"push": NewPushHandler(client, runs), "fetch": NewFetchHandler(client, runs)} {
		t.Run(name, func(t *testing.T) {
			resp := h.Handle(context.Background(), json.RawMessage(`{"task_id":"T"}`))
			if resp.IsError {
				t.Fatal(resp)
			}
			var body map[string]any
			if err := json.Unmarshal([]byte(resp.Content[0].Text), &body); err != nil {
				t.Fatal(err)
			}
			if name == "push" && (body["pushed"] != true || body["remote_sha"] != sha) {
				t.Fatal(body)
			}
			if name == "fetch" && body["fetched"] != true {
				t.Fatal(body)
			}
			if _, ok := body["run_id"]; ok {
				t.Fatalf("fast result returned handle: %v", body)
			}
		})
	}
}

func TestTransferHandlers_SlowPollAndRestart(t *testing.T) {
	for _, op := range []string{"push", "fetch"} {
		t.Run(op, func(t *testing.T) {
			client, src, bare, sha := transferFixture(t)
			// A remote helper blocks until released, so polling and duplicate
			// calls are checked while a real git operation remains in flight.
			release := filepath.Join(t.TempDir(), "release")
			marker := filepath.Join(t.TempDir(), "started")
			script := "#!/bin/sh\ntouch '" + marker + "'\nwhile [ ! -f '" + release + "' ]; do sleep 0.02; done\n"
			path := filepath.Join(bare, "hooks", "pre-receive")
			if op == "fetch" {
				path = filepath.Join(t.TempDir(), "upload-pack")
				script += "exec git-upload-pack \"$@\"\n"
				cmd := exec.Command("git", "-C", src, "config", "remote.origin.uploadpack", path)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("config: %v %s", err, out)
				}
			}
			if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.WriteFile(release, nil, 0o644) })
			runs := NewRunRegistry()
			var h Handler = NewPushHandler(client, runs)
			if op == "fetch" {
				h = NewFetchHandler(client, runs)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			input := json.RawMessage(`{"task_id":"T","wait_seconds":0}`)
			handle := decodeRunning(t, h.Handle(ctx, input))
			id := handle["run_id"].(string)
			cancel() // the operation must survive the initiating call.
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(marker); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("transfer never started: %+v", NewRunResultHandler(runs).Handle(context.Background(), json.RawMessage(`{"run_id":"`+id+`","wait_seconds":0}`)))
				}
				time.Sleep(10 * time.Millisecond)
			}
			again := decodeRunning(t, h.Handle(context.Background(), input))
			if again["run_id"] != id || again["status"] != "running" {
				t.Fatal(again)
			}
			// A nonzero bounded wait must return while the remote is blocked,
			// and wait duration must not change the deduplication identity.
			started := time.Now()
			bounded := decodeRunning(t, h.Handle(context.Background(), json.RawMessage(`{"task_id":"T","wait_seconds":1}`)))
			if bounded["run_id"] != id || time.Since(started) > 3*time.Second {
				t.Fatalf("bounded wait did not join promptly: %v", bounded)
			}
			var queuedIDs []string
			if op == "push" {
				// Each transfer option must be part of identity. These runs
				// queue behind the original push's source lock.
				for _, options := range []string{`"branch":"argus/other"`, `"remote":"missing"`, `"force_with_lease":true`} {
					other := decodeRunning(t, h.Handle(context.Background(), json.RawMessage(`{"task_id":"T","wait_seconds":0,`+options+`}`)))
					otherID := other["run_id"].(string)
					if otherID == id || other["status"] != "queued" {
						t.Fatalf("option ignored: %s: %v", options, other)
					}
					for _, prior := range queuedIDs {
						if otherID == prior {
							t.Fatalf("options shared identity: %s", otherID)
						}
					}
					queuedIDs = append(queuedIDs, otherID)
				}
			}
			poll := json.RawMessage(`{"run_id":"` + id + `","wait_seconds":0}`)
			decodeRunning(t, NewRunResultHandler(runs).Handle(context.Background(), poll))
			restarted := NewRunResultHandler(NewRunRegistry()).Handle(context.Background(), poll)
			if !restarted.IsError || !strings.Contains(restarted.Content[0].Text, "remote state") {
				t.Fatalf("unsafe restart guidance: %+v", restarted)
			}
			if err := os.WriteFile(release, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			final := NewRunResultHandler(runs).Handle(context.Background(), json.RawMessage(`{"run_id":"`+id+`","wait_seconds":5}`))
			if final.IsError {
				t.Fatal(final)
			}
			for _, otherID := range queuedIDs {
				// Drain queued runs before fixtures are removed. The deliberately
				// missing remote returns an error; the other pushes succeed.
				resp := NewRunResultHandler(runs).Handle(context.Background(), json.RawMessage(`{"run_id":"`+otherID+`","wait_seconds":5}`))
				if strings.Contains(resp.Content[0].Text, `"run_id"`) {
					t.Fatalf("queued run did not finish: %+v", resp)
				}
			}
			var body map[string]any
			if err := json.Unmarshal([]byte(final.Content[0].Text), &body); err != nil {
				t.Fatal(err)
			}
			if op == "push" && body["remote_sha"] != sha {
				t.Fatal(body)
			}
			if op == "fetch" {
				if _, ok := body["refs_updated"]; !ok || body["fetched"] != true {
					t.Fatal(body)
				}
			}
		})
	}
}

func TestTransferHandlers_AuthFailure(t *testing.T) {
	client, src, _, _ := transferFixture(t)
	helper := filepath.Join(t.TempDir(), "deny-auth")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\necho 'Permission denied (publickey).' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", src, "config", "remote.origin.receivepack", helper)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("config: %v %s", err, out)
	}
	runs := NewRunRegistry()
	handle := decodeRunning(t, NewPushHandler(client, runs).Handle(context.Background(), json.RawMessage(`{"task_id":"T","wait_seconds":0}`)))
	resp := NewRunResultHandler(runs).Handle(context.Background(), json.RawMessage(`{"run_id":"`+handle["run_id"].(string)+`","wait_seconds":5}`))
	if !resp.IsError || !strings.Contains(resp.Content[0].Text, "[auth_failure]") {
		t.Fatal(resp)
	}
	inline := NewPushHandler(client, runs).Handle(context.Background(), json.RawMessage(`{"task_id":"T","wait_seconds":5}`))
	if !inline.IsError || !strings.Contains(inline.Content[0].Text, "[auth_failure]") {
		t.Fatal(inline)
	}
}

func TestTransferHandlers_InvalidInput(t *testing.T) {
	for _, h := range []Handler{NewPushHandler(nil, NewRunRegistry()), NewFetchHandler(nil, NewRunRegistry())} {
		for _, input := range []string{`{`, `{}`, `{"task_id":"T","wait_seconds":"bad"}`} {
			if resp := h.Handle(context.Background(), json.RawMessage(input)); !resp.IsError {
				t.Fatalf("accepted %s: %+v", input, resp)
			}
		}
	}
}

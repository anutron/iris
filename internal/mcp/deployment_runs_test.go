package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeploymentHandlers_RunHandlesSurviveCancellation(t *testing.T) {
	for _, tool := range []string{"deploy", "set", "reload"} {
		t.Run(tool, func(t *testing.T) {
			t.Setenv("IRIS_AUDIT_DIR", t.TempDir())
			client, src, _, _ := transferFixture(t)
			release := filepath.Join(t.TempDir(), "release")
			marker := filepath.Join(t.TempDir(), "started")
			config := `schema_version=1
dogfood_branch="dev"
[build]
command=["sh","build.sh"]
[restart]
mechanism="none"
`
			script := "touch '" + marker + "'\nwhile [ ! -f '" + release + "' ]; do sleep 0.02; done\n"
			for name, content := range map[string]string{".iris.toml": config, "build.sh": script} {
				if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			git := func(args ...string) string {
				t.Helper()
				cmd := exec.Command("git", append([]string{"-C", src, "-c", "user.name=test", "-c", "user.email=test@example.com"}, args...)...)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git: %v %s", err, out)
				}
				return strings.TrimSpace(string(out))
			}
			git("add", ".")
			git("commit", "-m", "config")
			sha := git("rev-parse", "HEAD")
			registry := NewRunRegistry()
			var handler Handler
			input := map[string]any{"task_id": "T", "wait_seconds": 0, "lease_override": true}
			switch tool {
			case "deploy":
				handler = NewDogfoodDeployHandler(client, registry)
				input["ref"] = sha
			case "set":
				handler = NewSetDogfoodHandler(client, registry)
				input["sha"] = sha
				input["manifest"] = map[string]any{"base": map[string]string{"ref": "main", "sha": sha}}
			case "reload":
				handler = NewReloadHandler(client, registry)
				input["no_pull"] = true
			}
			t.Cleanup(func() { os.WriteFile(release, nil, 0600) })
			raw, _ := json.Marshal(input)
			ctx, cancel := context.WithCancel(context.Background())
			handle := decodeRunning(t, handler.Handle(ctx, raw))
			cancel()
			id := handle["run_id"].(string)
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(marker); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("never built: %+v", registry.Respond(context.Background(), registry.Get(id), 0))
				}
				time.Sleep(10 * time.Millisecond)
			}
			input["wait_seconds"] = 1
			raw, _ = json.Marshal(input)
			again := decodeRunning(t, handler.Handle(context.Background(), raw))
			if again["run_id"] != id || again["status"] != "running" {
				t.Fatal(again)
			}
			input["wait_seconds"] = 0
			if tool == "reload" {
				input["timeout_seconds"] = 1
			} else {
				input["force"] = true
			}
			otherRaw, _ := json.Marshal(input)
			other := decodeRunning(t, handler.Handle(context.Background(), otherRaw))
			otherID := other["run_id"].(string)
			if otherID == id {
				t.Fatal("different deployment options joined the same operation")
			}
			if err := os.WriteFile(release, nil, 0600); err != nil {
				t.Fatal(err)
			}
			poll := NewRunResultHandler(registry).Handle(context.Background(), json.RawMessage(`{"run_id":"`+id+`","wait_seconds":5}`))
			if poll.IsError || !strings.Contains(poll.Content[0].Text, sha) {
				t.Fatalf("final: %+v", poll)
			}
			otherPoll := NewRunResultHandler(registry).Handle(context.Background(), json.RawMessage(`{"run_id":"`+otherID+`","wait_seconds":5}`))
			if otherPoll.IsError || !strings.Contains(otherPoll.Content[0].Text, sha) {
				t.Fatalf("second run: %+v", otherPoll)
			}
		})
	}
}

func TestDeployHandler_RejectsMissingRefBeforeResolve(t *testing.T) {
	resp := NewDogfoodDeployHandler(nil, NewRunRegistry()).Handle(context.Background(), json.RawMessage(`{}`))
	if !resp.IsError || !strings.Contains(resp.Content[0].Text, "ref is required") {
		t.Fatal(resp)
	}
}

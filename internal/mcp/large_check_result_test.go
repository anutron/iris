package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anutron/iris/internal/verbs"
)

// Model the actual Argus proxy: its 4 MiB LimitReader truncates an oversized
// JSON document and json.Decoder reports unexpected EOF. All log data and
// check scripts here are synthetic; no host checks or daemons are invoked.
func TestLargeCheckResultThroughProxy(t *testing.T) {
	for _, exit := range []int{0, 1} {
		for _, poll := range []bool{false, true} {
			t.Run(fmt.Sprintf("exit=%d/poll=%t", exit, poll), func(t *testing.T) {
				t.Setenv("XDG_CACHE_HOME", t.TempDir())
				// macOS UserCacheDir uses HOME instead of XDG_CACHE_HOME.
				t.Setenv("HOME", t.TempDir())
				client, src, _, _ := transferFixture(t)
				wt := filepath.Join(filepath.Dir(src), "wt")
				log := strings.Repeat("synthetic progress\n", 300000) + "\nFailures:\n\n  1) synthetic regression\n     expected true, got false\n\nFinished in 1 second\n13300 examples, 1 failure, 2 pending\n" + strings.Repeat("epilogue\n", 10000) + "END OF LOG\n"
				if err := os.WriteFile(filepath.Join(wt, "synthetic.log"), []byte(log), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(wt, "script"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(wt, "script", "iris-check"), []byte(fmt.Sprintf("#!/bin/sh\ncat synthetic.log\nexit %d\n", exit)), 0o755); err != nil {
					t.Fatal(err)
				}
				runs := NewRunRegistry()
				h := NewRunChecksHandler(client, runs)
				input := json.RawMessage(`{"task_id":"T","check":"test","wait_seconds":5}`)
				if poll {
					input = json.RawMessage(`{"task_id":"T","check":"test","wait_seconds":0}`)
				}
				resp := h.Handle(context.Background(), input)
				if poll {
					handle := decodeRunning(t, resp)
					resp = NewRunResultHandler(runs).Handle(context.Background(), json.RawMessage(`{"run_id":"`+handle["run_id"].(string)+`","wait_seconds":5}`))
				}
				wire, err := json.Marshal(resp)
				if err != nil {
					t.Fatal(err)
				}
				var decoded Response
				if err := json.NewDecoder(io.LimitReader(bytes.NewReader(wire), 4<<20)).Decode(&decoded); err != nil {
					t.Fatalf("Argus proxy cannot decode %d-byte response: %v", len(wire), err)
				}
				if len(wire) >= 1<<20 {
					t.Fatalf("response too large: %d", len(wire))
				}
				if decoded.IsError != (exit != 0) {
					t.Fatalf("lost exit status: %+v", decoded)
				}
				var body verbs.RunChecksResult
				block := decoded.Content[len(decoded.Content)-1].Text
				if err := json.Unmarshal([]byte(block), &body); err != nil {
					t.Fatal(err)
				}
				if body.ExitCode != exit || body.Command != "script/iris-check test" || !body.OutputTruncated || body.OutputBytes != len(log) || len(body.Output) > 64<<10 || !strings.HasSuffix(body.Output, "END OF LOG\n") {
					t.Fatalf("invalid bounded result: %+v", body)
				}
				if !strings.Contains(body.FailureSection, "synthetic regression") || body.Summary == nil || body.Summary.Examples != 13300 || body.Summary.Failures != 1 || body.Summary.Pending != 2 {
					t.Fatalf("missing diagnostic summary")
				}
				saved, err := os.ReadFile(body.LogPath)
				if err != nil || string(saved) != log {
					t.Fatalf("full log not preserved: %v", err)
				}
				info, err := os.Stat(body.LogPath)
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("log must be private: %v", err)
				}
			})
		}
	}
}

func TestCallbackRejectsOversizedEncodedResponse(t *testing.T) {
	s := NewServer("", "Bearer test", nil)
	s.RegisterHandler("large", HandlerFunc(func(context.Context, json.RawMessage) Response {
		return TextResponse(strings.Repeat("\x00", 1<<20)) // JSON escaping counts too.
	}))
	req := httptest.NewRequest("POST", "/mcp/large", strings.NewReader(`{"tool":"large","input":{}}`))
	req.Header.Set("Authorization", "Bearer test")
	w := httptest.NewRecorder()
	s.handleCallback(w, req)
	var resp Response
	if err := json.NewDecoder(io.LimitReader(w.Body, 4<<20)).Decode(&resp); err != nil {
		t.Fatalf("corrupt proxy response: %v", err)
	}
	if !resp.IsError || !strings.Contains(resp.Content[0].Text, "response size limit") {
		t.Fatalf("expected clear size error")
	}
}

func TestCheckResultLogPersistenceFailure(t *testing.T) {
	t.Setenv("HOME", "/dev/null")
	t.Setenv("XDG_CACHE_HOME", "/dev/null")
	client, src, _, _ := transferFixture(t)
	wt := filepath.Join(filepath.Dir(src), "wt")
	full := strings.Repeat("synthetic progress\n", 10000)
	if err := os.WriteFile(filepath.Join(wt, "synthetic.log"), []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wt, "script"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "script", "iris-check"), []byte("#!/bin/sh\ncat synthetic.log\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	resp := runChecks(context.Background(), client, runChecksInput{TaskID: "T", Check: "test"})
	if !resp.IsError || !strings.Contains(resp.Content[0].Text, "preserve full check log") {
		t.Fatal("log persistence failure must be explicit")
	}
	var result verbs.RunChecksResult
	if len(resp.Content) != 2 || json.Unmarshal([]byte(resp.Content[1].Text), &result) != nil {
		t.Fatal("missing structured result on log persistence failure")
	}
	if result.ExitCode != 0 || !result.OutputTruncated || result.LogPath != "" || len(result.Output) > 64<<10 {
		t.Fatal("must retain actual exit code and bounded output without claiming a saved log")
	}
}

func TestSmallCheckResultShape(t *testing.T) {
	client, src, _, _ := transferFixture(t)
	wt := filepath.Join(filepath.Dir(src), "wt")
	if err := os.MkdirAll(filepath.Join(wt, "script"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, exit := range []int{0, 1} {
		if err := os.WriteFile(filepath.Join(wt, "script", "iris-check"), []byte(fmt.Sprintf("#!/bin/sh\nprintf 'small output\\n'\nexit %d\n", exit)), 0o755); err != nil {
			t.Fatal(err)
		}
		resp := runChecks(context.Background(), client, runChecksInput{TaskID: "T", Check: "test"})
		if len(resp.Content) != 1 || resp.IsError != (exit != 0) {
			t.Fatal("small response shape changed")
		}
		if exit != 0 {
			if resp.Content[0].Text != "iris:run_checks: script/iris-check test exited 1\nsmall output\n" {
				t.Fatal("small error message changed")
			}
			continue
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(resp.Content[0].Text), &body); err != nil {
			t.Fatal(err)
		}
		if len(body) != 3 || body["command"] != "script/iris-check test" || body["exit_code"] != float64(0) || body["output"] != "small output\n" {
			t.Fatal("small success fields changed")
		}
	}
}

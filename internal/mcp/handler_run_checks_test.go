package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Worktree-runs scenarios: args are part of the duplicate-run key, in order,
// and cannot collide across the check/arg boundary.
func TestRunChecksKey(t *testing.T) {
	t.Parallel()
	if runChecksKey("test", nil) != "test" {
		t.Fatalf("no args must keep the bare check as the key, got %q", runChecksKey("test", nil))
	}
	distinct := []struct {
		check string
		args  []string
	}{
		{"test", []string{"a"}},
		{"test", []string{"b"}},
		{"test", []string{"a", "b"}},
		{"test", []string{"b", "a"}},
		{"test", nil},
		{"a", []string{"b"}},
		{"a b", nil},
	}
	seen := map[string]int{}
	for i, d := range distinct {
		k := runChecksKey(d.check, d.args)
		if j, dup := seen[k]; dup {
			t.Errorf("key collision between cases %d and %d: %q", j, i, k)
		}
		seen[k] = i
	}
	if runChecksKey("test", []string{"a"}) != runChecksKey("test", []string{"a"}) {
		t.Error("identical check and args must share a key")
	}
}

// Invalid args fail immediately, before the task is resolved or a run is
// registered (a nil client would panic on resolve).
func TestRunChecksHandler_InvalidArgsFailBeforeStart(t *testing.T) {
	t.Parallel()
	reg := NewRunRegistry()
	h := NewRunChecksHandler(nil, reg)

	for name, args := range map[string][]string{
		"leading dash": {"--require", "evil.rb"},
		"empty arg":    {""},
		"NUL":          {"a\x00b"},
	} {
		in, _ := json.Marshal(map[string]any{"task_id": "T", "check": "test", "args": args})
		resp := h.Handle(context.Background(), in)
		if !resp.IsError || !strings.Contains(resp.Content[0].Text, "args") {
			t.Errorf("%s: expected an args error, got %+v", name, resp)
		}
	}
}

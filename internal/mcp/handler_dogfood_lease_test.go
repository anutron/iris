package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anutron/iris/internal/verbs"
)

func TestLeaseErr_GateErrorCarriesStructuredFields(t *testing.T) {
	ge := &verbs.LeaseGateError{
		Message:  "refusing to deploy: held by task-a",
		Holder:   &verbs.LeaseRef{TaskID: "A", TaskName: "task-a"},
		Ahead:    []verbs.LeaseRef{{TaskID: "B", TaskName: "task-b", Position: 1}},
		Position: 2,
	}
	resp := leaseErr("iris:set_dogfood", ge)
	if !resp.IsError || len(resp.Content) != 2 {
		t.Fatalf("resp = %+v", resp)
	}
	if !strings.Contains(resp.Content[0].Text, "held by task-a") {
		t.Fatalf("text block = %q", resp.Content[0].Text)
	}
	var f struct {
		Holder   verbs.LeaseRef   `json:"holder"`
		Ahead    []verbs.LeaseRef `json:"ahead"`
		Position int              `json:"position"`
	}
	if err := json.Unmarshal([]byte(resp.Content[1].Text), &f); err != nil {
		t.Fatalf("second block must be JSON: %v", err)
	}
	if f.Holder.TaskID != "A" || len(f.Ahead) != 1 || f.Ahead[0].TaskName != "task-b" || f.Position != 2 {
		t.Fatalf("fields = %+v", f)
	}
}

func TestLeaseErr_RenewRefusalIsStructuredToo(t *testing.T) {
	ref := &verbs.LeaseRefusal{Holder: &verbs.LeaseRef{TaskID: "A", TaskName: "task-a"}, Ahead: []verbs.LeaseRef{}, Position: 1}
	resp := leaseErr("iris:dogfood_renew", ref)
	if len(resp.Content) != 2 || !strings.Contains(resp.Content[1].Text, `"position":1`) {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestLeaseErr_PlainErrorIsSingleBlock(t *testing.T) {
	resp := leaseErr("iris:dogfood_claim", errors.New("boom"))
	if !resp.IsError || len(resp.Content) != 1 || resp.Content[0].Text != "iris:dogfood_claim: boom" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestDogfoodHandlers_RejectMalformedInput(t *testing.T) {
	for name, h := range map[string]Handler{
		"claim": NewDogfoodClaimHandler(nil), "renew": NewDogfoodRenewHandler(nil),
		"release": NewDogfoodReleaseHandler(nil), "force": NewDogfoodForceReleaseHandler(nil),
		"move": NewDogfoodQueueMoveHandler(nil),
	} {
		resp := h.Handle(t.Context(), json.RawMessage(`{"task_id": 5`))
		if !resp.IsError || !strings.Contains(resp.Content[0].Text, "invalid input") {
			t.Errorf("%s: resp = %+v", name, resp)
		}
	}
}

func TestDogfoodHandlers_RequireTaskID(t *testing.T) {
	for name, h := range map[string]Handler{
		"claim": NewDogfoodClaimHandler(nil), "renew": NewDogfoodRenewHandler(nil), "release": NewDogfoodReleaseHandler(nil),
	} {
		resp := h.Handle(t.Context(), json.RawMessage(`{}`))
		if !resp.IsError || !strings.Contains(resp.Content[0].Text, "task_id is required") {
			t.Errorf("%s: resp = %+v", name, resp)
		}
	}
}

func TestDogfoodClaimInput_OptionalSHA(t *testing.T) {
	var in dogfoodClaimInput
	if r := decodeInput("iris:dogfood_claim", json.RawMessage(`{"task_id":"B","sha":"main"}`), &in); r != nil || in.SHA != "main" {
		t.Fatalf("input=%+v response=%+v", in, r)
	}
	resp := NewDogfoodClaimHandler(nil).Handle(t.Context(), json.RawMessage(`{"task_id":"B","sha":123}`))
	if !resp.IsError || !strings.Contains(resp.Content[0].Text, "invalid input") {
		t.Fatalf("invalid SHA type accepted: %+v", resp)
	}
}

func TestDogfoodClaimHandler_ContainedSHA(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client, src, _, sha := transferFixture(t)
	if err := os.WriteFile(filepath.Join(src, ".iris.toml"), []byte("schema_version = 1\ndogfood_branch = \"dev\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", src, "branch", "dev", sha).CombinedOutput(); err != nil {
		t.Fatalf("branch: %v %s", err, out)
	}
	resp := NewDogfoodClaimHandler(client).Handle(t.Context(), json.RawMessage(`{"task_id":"T","sha":"`+sha+`"}`))
	if resp.IsError {
		t.Fatal(resp)
	}
	var result verbs.DogfoodClaimResult
	if err := json.Unmarshal([]byte(resp.Content[0].Text), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "already_deployed" || result.SHA != sha || result.Holder != nil || result.NextCheckinAt != nil {
		t.Fatalf("result=%+v", result)
	}
}

package daemon

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolDefinitions_DogfoodLeaseTools(t *testing.T) {
	defs := map[string]map[string]any{}
	for _, d := range toolDefinitions() {
		b, _ := json.Marshal(d.InputSchema)
		var schema map[string]any
		_ = json.Unmarshal(b, &schema)
		defs[d.Name] = schema
		if d.Name == "iris_set_dogfood" || d.Name == "iris_dogfood_claim" || d.Name == "iris_dogfood_renew" {
			for _, want := range []string{"Don't hold dogfood for a user who is away.", "Don't hold dogfood while fixing a problem you found:", "get back in line with a new iris_dogfood_claim", "If the user is away and Iris asks for dogfood back or says someone is waiting, call iris_dogfood_release right away."} {
				if !strings.Contains(d.Description, want) {
					t.Errorf("%s description missing %q", d.Name, want)
				}
			}
		}
		if d.Name == "iris_set_dogfood" {
			for _, want := range []string{"iris_dogfood_claim", "lease_override", "force"} {
				if !strings.Contains(d.Description, want) {
					t.Errorf("iris_set_dogfood description missing %q", want)
				}
			}
		}
	}
	for _, name := range []string{"iris_dogfood_claim", "iris_dogfood_renew", "iris_dogfood_release", "iris_dogfood_force_release", "iris_dogfood_queue_move"} {
		if defs[name] == nil {
			t.Errorf("tool %s not registered", name)
		}
	}
	props := defs["iris_set_dogfood"]["properties"].(map[string]any)
	if props["lease_override"] == nil || props["force"] == nil {
		t.Fatalf("set_dogfood schema must have both lease_override and force: %v", props)
	}
	claim := defs["iris_dogfood_claim"]["properties"].(map[string]any)
	for _, k := range []string{"task_id", "sha", "wait_seconds", "position"} {
		if claim[k] == nil {
			t.Errorf("claim schema missing %s", k)
		}
	}
	req := defs["iris_dogfood_force_release"]["required"].([]any)
	if len(req) != 1 || req[0] != "reason" {
		t.Errorf("force_release must require reason: %v", req)
	}
}

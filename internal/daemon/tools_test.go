package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anutron/iris/internal/argus"
	"github.com/anutron/iris/internal/mcp"
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

func TestRegistrar_DeployCatalogRoundTrip(t *testing.T) {
	var mu sync.Mutex
	registered := map[string]argus.MCPTool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var tool argus.MCPTool
			if err := json.NewDecoder(r.Body).Decode(&tool); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			mu.Lock()
			registered[tool.Name] = tool
			mu.Unlock()
			json.NewEncoder(w).Encode(map[string]string{"name": tool.Name, "scope": "iris"})
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	registrar := mcp.NewRegistrar(argus.New(server.URL, "test"), "http://callback.invalid", "Bearer test", nil)
	for _, def := range toolDefinitions() {
		registrar.Add(def)
	}
	if err := registrar.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer registrar.Stop(context.Background())
	mu.Lock()
	defer mu.Unlock()
	tool, ok := registered["iris_dogfood_deploy"]
	if !ok || tool.CallbackURL != "http://callback.invalid/mcp/iris_dogfood_deploy" {
		t.Fatalf("deploy registration=%+v", tool)
	}
	props := tool.InputSchema["properties"].(map[string]any)
	if props["ref"] == nil || props["wait_seconds"] == nil {
		t.Fatalf("deploy schema lost: %+v", tool)
	}
}

func TestToolDefinitions_DeploymentRuns(t *testing.T) {
	defs := map[string]map[string]any{}
	for _, def := range toolDefinitions() {
		defs[def.Name] = def.InputSchema
	}
	for _, name := range []string{"iris_dogfood_deploy", "iris_set_dogfood", "iris_reload"} {
		if defs[name] == nil {
			t.Fatalf("missing %s", name)
		}
		props := defs[name]["properties"].(map[string]any)
		if props["wait_seconds"] == nil {
			t.Errorf("%s missing bounded wait", name)
		}
	}
	props := defs["iris_dogfood_deploy"]["properties"].(map[string]any)
	for _, key := range []string{"ref", "compose", "force", "lease_override", "task_id"} {
		if props[key] == nil {
			t.Errorf("deploy missing %s", key)
		}
	}
}

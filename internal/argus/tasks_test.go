package argus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListTasks_DecodesEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tasks" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tasks": []map[string]any{
				{
					"id":            "abc",
					"name":          "first",
					"project":       "iris",
					"status":        "in_progress",
					"worktree_path": "/tmp/wt-1",
					"branch":        "argus/feature-x",
				},
				{
					"id":            "def",
					"name":          "second",
					"project":       "other",
					"status":        "complete",
					"worktree_path": "/tmp/wt-2",
					"branch":        "argus/feature-y",
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "stub")
	tasks, err := c.ListTasks(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d: %#v", len(tasks), tasks)
	}
	if tasks[0].ID != "abc" || tasks[0].WorktreePath != "/tmp/wt-1" || tasks[0].Branch != "argus/feature-x" {
		t.Fatalf("first task decoded wrong: %#v", tasks[0])
	}
	if tasks[1].ID != "def" || tasks[1].Status != "complete" {
		t.Fatalf("second task decoded wrong: %#v", tasks[1])
	}
}

func TestListTasks_EmptyEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{}})
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "stub")
	tasks, err := c.ListTasks(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected empty list, got %#v", tasks)
	}
}

func TestListTasks_HTTPErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "stub")
	_, err := c.ListTasks(context.Background())
	if err == nil {
		t.Fatal("expected an error for 500 response")
	}
}

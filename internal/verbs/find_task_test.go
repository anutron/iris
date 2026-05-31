package verbs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/anutron/iris/internal/argus"
)

// stubArgusTaskList builds a client backed by a fake argus that answers
// GET /api/tasks with the given task slice. /api/projects/full also
// answers (empty list) so callers that hit it don't 404.
func stubArgusTaskList(t *testing.T, tasks []map[string]any) *argus.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks})
		case "/api/projects/full":
			_ = json.NewEncoder(w).Encode(map[string]any{"projects": []map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return argus.New(srv.URL, "stub-token")
}

func TestFindTaskBySourceRepo_Match(t *testing.T) {
	t.Parallel()
	src := setupRepoOnly(t)
	canon, _ := filepath.EvalSymlinks(src)
	client := stubArgusTaskList(t, []map[string]any{
		{"id": "other", "worktree_path": "/nope/elsewhere"},
		{"id": "match", "worktree_path": canon, "branch": "argus/feature-x", "name": "n", "project": "iris", "status": "in_progress"},
	})
	got, err := FindTaskBySourceRepo(context.Background(), client, src)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got == nil {
		t.Fatal("expected a match")
	}
	if got.ID != "match" {
		t.Fatalf("got id=%q want match", got.ID)
	}
}

func TestFindTaskBySourceRepo_NoMatchReturnsNilNil(t *testing.T) {
	t.Parallel()
	src := setupRepoOnly(t)
	client := stubArgusTaskList(t, []map[string]any{
		{"id": "elsewhere", "worktree_path": "/nope/elsewhere"},
	})
	got, err := FindTaskBySourceRepo(context.Background(), client, src)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil task, got %#v", got)
	}
}

func TestFindTaskBySourceRepo_EmptyList(t *testing.T) {
	t.Parallel()
	src := setupRepoOnly(t)
	client := stubArgusTaskList(t, []map[string]any{})
	got, err := FindTaskBySourceRepo(context.Background(), client, src)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil task, got %#v", got)
	}
}

func TestFindTaskBySourceRepo_ArgusError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	client := argus.New(srv.URL, "stub")
	got, err := FindTaskBySourceRepo(context.Background(), client, "/tmp/anything")
	if err == nil {
		t.Fatal("expected error when argus 500s")
	}
	if got != nil {
		t.Fatalf("expected nil task on error, got %#v", got)
	}
}

func TestFindTaskBySourceRepo_NilClient(t *testing.T) {
	t.Parallel()
	got, err := FindTaskBySourceRepo(context.Background(), nil, "/some/path")
	if err != nil {
		t.Fatalf("expected no error for nil client, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil task, got %#v", got)
	}
}

func TestFindTaskBySourceRepo_SkipsEmptyWorktreePath(t *testing.T) {
	t.Parallel()
	src := setupRepoOnly(t)
	canon, _ := filepath.EvalSymlinks(src)
	client := stubArgusTaskList(t, []map[string]any{
		{"id": "no-path", "worktree_path": ""},
		{"id": "match", "worktree_path": canon},
	})
	got, err := FindTaskBySourceRepo(context.Background(), client, src)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got == nil || got.ID != "match" {
		t.Fatalf("expected match id, got %#v", got)
	}
}

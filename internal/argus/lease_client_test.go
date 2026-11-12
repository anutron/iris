package argus

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetBridge_Decodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tasks/t1/bridge" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"live": true, "busy": true, "needs_input": false,
			"last_event_at": "2026-01-02T03:04:05Z", "last_heartbeat_at": "2026-01-02T03:04:06Z",
		})
	}))
	t.Cleanup(srv.Close)
	b, err := New(srv.URL, "x").GetBridge(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	if !b.Live || !b.Busy || b.NeedsInput || b.LastEventAt.IsZero() || b.LastHeartbeatAt.IsZero() {
		t.Fatalf("bridge = %+v", b)
	}
}

func TestGetBridge_404IsNoBridge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no bridge state for task", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	b, err := New(srv.URL, "x").GetBridge(context.Background(), "t1")
	if !errors.Is(err, ErrNoBridge) || b != nil {
		t.Fatalf("b=%v err=%v, want ErrNoBridge", b, err)
	}
}

func TestGetBridge_5xxIsNotNoBridge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	_, err := New(srv.URL, "x").GetBridge(context.Background(), "t1")
	if err == nil || errors.Is(err, ErrNoBridge) {
		t.Fatalf("err = %v", err)
	}
}

func TestGetTask_ArchivedAndNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tasks/a":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "a", "name": "alpha", "status": "in_progress", "archived": true})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "x")
	tk, err := c.GetTask(context.Background(), "a")
	if err != nil || !tk.Archived || tk.Name != "alpha" {
		t.Fatalf("task=%+v err=%v", tk, err)
	}
	_, err = c.GetTask(context.Background(), "gone")
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound(%v) = false", err)
	}
	if IsNotFound(errors.New("other")) || IsNotFound(nil) {
		t.Fatal("IsNotFound false positives")
	}
}

func TestNotify_PostsBody(t *testing.T) {
	var gotPath, gotMethod string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"delivery_id":"iris-lease-x-1","state":"pending"}`))
	}))
	t.Cleanup(srv.Close)
	state, err := New(srv.URL, "x").Notify(context.Background(), "t1", "[iris] hi", "iris-lease-x-1", 90*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if state != "pending" {
		t.Fatalf("state = %q, want pending", state)
	}
	if gotMethod != "POST" || gotPath != "/api/tasks/t1/notify" {
		t.Fatalf("%s %s", gotMethod, gotPath)
	}
	if got["text"] != "[iris] hi" || got["submit"] != true || got["delivery_id"] != "iris-lease-x-1" || got["deadline_ms"] != float64(90000) {
		t.Fatalf("body = %v", got)
	}
}

func TestNotify_404IsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }))
	t.Cleanup(srv.Close)
	_, err := New(srv.URL, "x").Notify(context.Background(), "t1", "x", "d", time.Second)
	if !IsNotFound(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestCancelNotify_Delete(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	if err := New(srv.URL, "x").CancelNotify(context.Background(), "t1", "d-1"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != "DELETE" || gotPath != "/api/tasks/t1/notify/d-1" {
		t.Fatalf("%s %s", gotMethod, gotPath)
	}
}

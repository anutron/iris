package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anutron/iris/internal/argus"
)

// Host-bridge scenario: "Tools re-register on heartbeat."
func TestRegistrar_HeartbeatRePOSTs(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/mcp/tools" && r.Method == "POST" {
			posts.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"name": "iris_test", "scope": "iris"})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := argus.New(srv.URL, "tok")
	reg := NewRegistrar(client, "http://example.invalid", "Bearer t", nil)
	reg.SetHeartbeat(50 * time.Millisecond)
	reg.Add(ToolDefinition{Name: "iris_test", Description: "test tool"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := reg.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for at least 3 POSTs: initial + 2 ticks.
	deadline := time.Now().Add(2 * time.Second)
	for posts.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	got := posts.Load()
	if got < 3 {
		t.Fatalf("expected >= 3 POSTs (initial + heartbeats), got %d", got)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	_ = reg.Stop(stopCtx)
}

// Host-bridge: "Tools unregister on shutdown" — registrar issues DELETE
// for each tool on Stop.
func TestRegistrar_UnregistersOnStop(t *testing.T) {
	var posts, deletes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/mcp/tools" && r.Method == "POST":
			posts.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"name": "iris_test", "scope": "iris"})
		case strings.HasPrefix(r.URL.Path, "/api/mcp/tools/") && r.Method == "DELETE":
			deletes.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := argus.New(srv.URL, "tok")
	reg := NewRegistrar(client, "http://example.invalid", "Bearer t", nil)
	reg.SetHeartbeat(time.Hour) // suppress heartbeats during the test
	reg.Add(ToolDefinition{Name: "iris_test", Description: "test tool"})

	if err := reg.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if posts.Load() != 1 {
		t.Fatalf("initial registration: got %d POSTs, want 1", posts.Load())
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := reg.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if deletes.Load() != 1 {
		t.Fatalf("expected 1 DELETE on Stop, got %d", deletes.Load())
	}
}

// Host-bridge: heartbeat 404 fires the recovery callback.
func TestRegistrar_Heartbeat404FiresCallback(t *testing.T) {
	var firstPOSTSeen atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/mcp/tools" && r.Method == "POST" {
			if firstPOSTSeen.Swap(true) {
				// All POSTs after the first 404 to simulate argus having
				// garbage-collected the registration.
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"tool not found"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"name": "iris_test"})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := argus.New(srv.URL, "tok")
	reg := NewRegistrar(client, "http://example.invalid", "Bearer t", nil)
	reg.SetHeartbeat(30 * time.Millisecond)
	reg.Add(ToolDefinition{Name: "iris_test", Description: "test tool"})

	var cbFired atomic.Int32
	reg.SetOnHeartbeat404(func(ctx context.Context) {
		cbFired.Add(1)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := reg.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for cbFired.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if cbFired.Load() == 0 {
		t.Fatal("heartbeat 404 callback never fired")
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	_ = reg.Stop(stopCtx)
}

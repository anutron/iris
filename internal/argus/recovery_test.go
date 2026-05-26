package argus

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// stubReregistrar records ForceReregister invocations and can be configured
// to fail with a fixed error.
type stubReregistrar struct {
	calls atomic.Int32
	err   error
}

func (s *stubReregistrar) ForceReregister(ctx context.Context) error {
	s.calls.Add(1)
	return s.err
}

// startSocketServer launches a unix-RPC server speaking iris's Daemon.Ports
// over the socket. portsResp / pongResp / Empty mirror argus's shapes.
type fakeDaemon struct {
	apiPort int
	mcpPort int
}

func (f *fakeDaemon) Ports(_ *struct{}, out *struct{ MCPPort, APIPort int }) error {
	out.APIPort = f.apiPort
	out.MCPPort = f.mcpPort
	return nil
}

func (f *fakeDaemon) Ping(_ *struct{}, out *struct{ OK bool }) error {
	out.OK = true
	return nil
}

func startSocketServer(t *testing.T, apiPort int) string {
	t.Helper()
	// Unix sockets cap at 104 chars on macOS / 108 on Linux. t.TempDir's
	// default location (/var/folders/...<long-test-name>/...) easily blows
	// past that, so put the socket in /tmp with a short unique name.
	dir, err := os.MkdirTemp("/tmp", "iris-rec-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	sockPath := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() {
		_ = ln.Close()
		_ = os.RemoveAll(dir)
	})

	srv := rpc.NewServer()
	_ = srv.RegisterName("Daemon", &fakeDaemon{apiPort: apiPort})

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Argus's socket protocol: first byte is dispatch ('R').
			buf := make([]byte, 1)
			if _, err := conn.Read(buf); err != nil || buf[0] != 'R' {
				_ = conn.Close()
				continue
			}
			go srv.ServeCodec(jsonrpc.NewServerCodec(conn))
		}
	}()
	return sockPath
}

// Recovery happy path: ports query succeeds, ForceReregister succeeds,
// link transitions LinkRecovering → LinkHealthy.
func TestRecover_HappyPath(t *testing.T) {
	resetLinkState(t)

	// Start an httptest argus that accepts any request (no calls expected;
	// the client just needs a URL).
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer httpSrv.Close()

	// Discover the apiPort the socket should report.
	apiPort := portFromURL(t, httpSrv.URL)
	sockPath := startSocketServer(t, apiPort)

	ports := NewPortsClient(sockPath)
	client := New("http://127.0.0.1:1", "tok") // wrong URL; recovery should overwrite
	reg := &stubReregistrar{}

	Recover(context.Background(), ports, client, reg, nil)

	if got := GetLinkState(); got != LinkHealthy {
		t.Fatalf("LinkState after recover = %v, want LinkHealthy", got)
	}
	if err := LinkLastError(); err != nil {
		t.Fatalf("LinkLastError after recover = %v, want nil", err)
	}
	wantURL := fmt.Sprintf("http://127.0.0.1:%d", apiPort)
	if client.BaseURL() != wantURL {
		t.Fatalf("client.BaseURL = %q, want %q", client.BaseURL(), wantURL)
	}
	if reg.calls.Load() != 1 {
		t.Fatalf("ForceReregister called %d times, want 1", reg.calls.Load())
	}
}

// Recovery fails on ports query → LinkDown with wrapped error.
func TestRecover_PortsFailure(t *testing.T) {
	resetLinkState(t)

	// Point at a non-existent socket so ports.Ports errors immediately.
	ports := NewPortsClient(filepath.Join(t.TempDir(), "missing.sock"))
	client := New("http://127.0.0.1:1", "tok")
	reg := &stubReregistrar{}

	Recover(context.Background(), ports, client, reg, nil)

	if got := GetLinkState(); got != LinkDown {
		t.Fatalf("LinkState after recover = %v, want LinkDown", got)
	}
	err := LinkLastError()
	if err == nil {
		t.Fatal("LinkLastError should be set after ports failure")
	}
	if !strings.Contains(err.Error(), "ports query") {
		t.Fatalf("expected wrapped error to mention 'ports query', got: %v", err)
	}
	if reg.calls.Load() != 0 {
		t.Fatalf("ForceReregister should not run after ports failure, got %d calls", reg.calls.Load())
	}
}

// Recovery fails on reregister → LinkDown with wrapped error.
func TestRecover_ReregisterFailure(t *testing.T) {
	resetLinkState(t)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer httpSrv.Close()
	apiPort := portFromURL(t, httpSrv.URL)
	sockPath := startSocketServer(t, apiPort)

	ports := NewPortsClient(sockPath)
	client := New("http://127.0.0.1:1", "tok")
	reg := &stubReregistrar{err: errors.New("argus rejected")}

	Recover(context.Background(), ports, client, reg, nil)

	if got := GetLinkState(); got != LinkDown {
		t.Fatalf("LinkState after recover = %v, want LinkDown", got)
	}
	err := LinkLastError()
	if err == nil {
		t.Fatal("LinkLastError should be set after reregister failure")
	}
	if !strings.Contains(err.Error(), "mcp re-register") {
		t.Fatalf("expected wrapped error to mention 'mcp re-register', got: %v", err)
	}
	// Base URL was updated before reregister failed.
	if !strings.Contains(client.BaseURL(), fmt.Sprintf(":%d", apiPort)) {
		t.Fatalf("client.BaseURL should reflect discovered port, got %q", client.BaseURL())
	}
}

// RecoverFunc returns a closure that captures all dependencies and runs
// Recover when invoked.
func TestRecoverFunc_BindsDependencies(t *testing.T) {
	resetLinkState(t)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer httpSrv.Close()
	apiPort := portFromURL(t, httpSrv.URL)
	sockPath := startSocketServer(t, apiPort)

	ports := NewPortsClient(sockPath)
	client := New("http://127.0.0.1:1", "tok")
	reg := &stubReregistrar{}

	fn := RecoverFunc(ports, client, reg, nil)
	fn(context.Background())

	if got := GetLinkState(); got != LinkHealthy {
		t.Fatalf("LinkState = %v, want LinkHealthy", got)
	}
	if reg.calls.Load() != 1 {
		t.Fatalf("ForceReregister called %d times, want 1", reg.calls.Load())
	}
}

// portFromURL extracts the integer port from an httptest server URL.
func portFromURL(t *testing.T, u string) int {
	t.Helper()
	// u is "http://127.0.0.1:NNNN"
	idx := strings.LastIndex(u, ":")
	if idx < 0 {
		t.Fatalf("no port in URL %q", u)
	}
	var port int
	if _, err := fmt.Sscanf(u[idx+1:], "%d", &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return port
}

// resetLinkState restores the package-level state before/after a test.
// Tests that touch link state share the singleton; reset both sides so
// ordering doesn't leak.
func resetLinkState(t *testing.T) {
	t.Helper()
	SetLinkState(LinkHealthy)
	SetLinkError(nil)
	t.Cleanup(func() {
		SetLinkState(LinkHealthy)
		SetLinkError(nil)
	})
}

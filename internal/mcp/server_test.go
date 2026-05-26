package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// Host-bridge scenario: "Inbound MCP callback rejects wrong token."
func TestServer_RejectsWrongAuthHeader(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "Bearer correct", nil)
	var called atomic.Int32
	srv.RegisterHandler("iris_test", HandlerFunc(func(ctx context.Context, in json.RawMessage) Response {
		called.Add(1)
		return TextResponse("ok")
	}))
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	url := srv.CallbackBaseURL() + "/mcp/iris_test"
	req, _ := http.NewRequest("POST", url, strings.NewReader(`{"tool":"iris_test","input":{}}`))
	req.Header.Set("Authorization", "Bearer WRONG")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", resp.StatusCode)
	}
	if called.Load() != 0 {
		t.Fatalf("handler invoked despite auth failure (called=%d)", called.Load())
	}
}

func TestServer_AcceptsCorrectAuthHeader(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "Bearer correct", nil)
	var called atomic.Int32
	srv.RegisterHandler("iris_test", HandlerFunc(func(ctx context.Context, in json.RawMessage) Response {
		called.Add(1)
		return TextResponse("ok")
	}))
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	url := srv.CallbackBaseURL() + "/mcp/iris_test"
	req, _ := http.NewRequest("POST", url, strings.NewReader(`{"tool":"iris_test","input":{}}`))
	req.Header.Set("Authorization", "Bearer correct")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", resp.StatusCode, body)
	}
	if called.Load() != 1 {
		t.Fatalf("handler invoked %d times, want 1", called.Load())
	}
	var got Response
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode body: %v\n%s", err, body)
	}
	if got.IsError {
		t.Fatalf("unexpected isError=true: %+v", got)
	}
}

func TestServer_RejectsMissingAuthHeader(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "Bearer correct", nil)
	srv.RegisterHandler("iris_test", HandlerFunc(func(ctx context.Context, _ json.RawMessage) Response {
		t.Fatal("handler must not be invoked")
		return Response{}
	}))
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	req, _ := http.NewRequest("POST", srv.CallbackBaseURL()+"/mcp/iris_test", strings.NewReader(`{}`))
	// Deliberately no Authorization header.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", resp.StatusCode)
	}
}

func TestServer_UnknownToolReturns404(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "Bearer t", nil)
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	req, _ := http.NewRequest("POST", srv.CallbackBaseURL()+"/mcp/iris_nope", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer t")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", resp.StatusCode)
	}
}

func TestServer_MethodNotAllowed(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "Bearer t", nil)
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	resp, err := http.Get(srv.CallbackBaseURL() + "/mcp/anything")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d, want 405", resp.StatusCode)
	}
}

func TestServer_OversizedBodyRejected(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "Bearer t", nil)
	srv.RegisterHandler("iris_test", HandlerFunc(func(ctx context.Context, _ json.RawMessage) Response {
		t.Fatal("handler must not be invoked for oversized body")
		return Response{}
	}))
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	// 2 MiB body — beyond the 1 MiB MaxBytesReader cap.
	big := bytes.Repeat([]byte("x"), 2<<20)
	body := append([]byte(`{"tool":"iris_test","input":"`), big...)
	body = append(body, []byte(`"}`)...)
	req, _ := http.NewRequest("POST", srv.CallbackBaseURL()+"/mcp/iris_test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer t")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status: got %d, want 400 or 413", resp.StatusCode)
	}
}

func TestGenerateAuthHeader_Format(t *testing.T) {
	for i := 0; i < 5; i++ {
		h, err := GenerateAuthHeader()
		if err != nil {
			t.Fatalf("GenerateAuthHeader: %v", err)
		}
		if !strings.HasPrefix(h, "Bearer ") {
			t.Fatalf("auth header missing 'Bearer ' prefix: %q", h)
		}
		// 64 hex chars = 32 bytes of entropy.
		if len(h) != len("Bearer ")+64 {
			t.Fatalf("auth header wrong length: %d (%q)", len(h), h)
		}
	}
}

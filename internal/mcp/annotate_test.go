package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/anutron/iris/internal/verbs"
)

var testRef = &verbs.RepoRef{Name: "acme/video-gen", Path: "/src/video-gen"}

func TestAnnotateRepo_IndentedJSONGetsRepoFirst(t *testing.T) {
	t.Parallel()
	body, _ := json.MarshalIndent(map[string]any{"merged": true, "strategy": "squash"}, "", "  ")
	got := AnnotateRepo(TextResponse(string(body)), testRef)

	text := got.Content[0].Text
	if !strings.HasPrefix(text, "{\n  \"repo\": {\"name\":\"acme/video-gen\",\"path\":\"/src/video-gen\"},\n") {
		t.Fatalf("repo not injected first:\n%s", text)
	}
	if !strings.HasSuffix(text, string(body)[1:]) {
		t.Fatalf("original body not preserved verbatim:\n%s", text)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("annotated body is not valid JSON: %v\n%s", err, text)
	}
	if decoded["merged"] != true {
		t.Fatalf("existing field lost: %v", decoded)
	}
}

func TestAnnotateRepo_CompactAndEmptyObjects(t *testing.T) {
	t.Parallel()
	for _, in := range []string{`{"a":1}`, `{}`, "{\n}"} {
		got := AnnotateRepo(TextResponse(in), testRef).Content[0].Text
		var decoded struct {
			Repo verbs.RepoRef `json:"repo"`
		}
		if err := json.Unmarshal([]byte(got), &decoded); err != nil {
			t.Fatalf("input %q produced invalid JSON %q: %v", in, got, err)
		}
		if decoded.Repo != *testRef {
			t.Fatalf("input %q: repo = %+v", in, decoded.Repo)
		}
	}
}

func TestAnnotateRepo_ErrorGetsTrailingLine(t *testing.T) {
	t.Parallel()
	got := AnnotateRepo(ErrorResponse("iris:gh_pr_merge: gh pr merge: not mergeable\n"), testRef)
	if !got.IsError {
		t.Fatal("annotation must not clear isError")
	}
	want := "iris:gh_pr_merge: gh pr merge: not mergeable\nrepo: acme/video-gen (/src/video-gen)"
	if got.Content[0].Text != want {
		t.Fatalf("got %q, want %q", got.Content[0].Text, want)
	}
}

func TestAnnotateRepo_PathOnlyAndNonJSON(t *testing.T) {
	t.Parallel()
	got := AnnotateRepo(TextResponse("plain text"), &verbs.RepoRef{Path: "/src/x"})
	if got.Content[0].Text != "plain text\nrepo: /src/x" {
		t.Fatalf("got %q", got.Content[0].Text)
	}
}

func TestAnnotateRepo_JSONBodiesAreNeverAppendedTo(t *testing.T) {
	t.Parallel()
	for _, in := range []string{`{"repo":"mine"}`, `[1,2]`, `"text"`, `null`} {
		if got := AnnotateRepo(TextResponse(in), testRef).Content[0].Text; got != in {
			t.Errorf("JSON body %q must be left alone, got %q", in, got)
		}
		if got := AnnotateRepo(ErrorResponse(in), testRef).Content[0].Text; got != in {
			t.Errorf("JSON error body %q must be left alone, got %q", in, got)
		}
	}
}

// A JSON error envelope (set_local_config's {code, field, message, hint})
// stays decodable and gains repo like a success body.
func TestAnnotateRepo_JSONErrorEnvelopeGetsRepoKey(t *testing.T) {
	t.Parallel()
	in := "{\n  \"code\": \"field_not_local\",\n  \"field\": \"default_branch\"\n}"
	got := AnnotateRepo(ErrorResponse(in), testRef)
	if !got.IsError {
		t.Fatal("annotation must not clear isError")
	}
	var env struct {
		Repo  verbs.RepoRef `json:"repo"`
		Code  string        `json:"code"`
		Field string        `json:"field"`
	}
	if err := json.Unmarshal([]byte(got.Content[0].Text), &env); err != nil {
		t.Fatalf("error envelope no longer valid JSON: %v\n%s", err, got.Content[0].Text)
	}
	if env.Code != "field_not_local" || env.Field != "default_branch" || env.Repo != *testRef {
		t.Fatalf("envelope damaged: %+v", env)
	}
}

// Check and build output inside an error must reach the caller byte for byte.
func TestAnnotateRepo_PreservesOutputVerbatim(t *testing.T) {
	t.Parallel()
	in := "iris:run_checks: script/iris-check test exited 1\nFAIL x\n  \n\n"
	got := AnnotateRepo(ErrorResponse(in), testRef).Content[0].Text
	if got != in+"repo: acme/video-gen (/src/video-gen)" {
		t.Fatalf("output altered: %q", got)
	}
}

func TestAnnotateRepo_NilRefUnchanged(t *testing.T) {
	t.Parallel()
	in := TextResponse(`{"a":1}`)
	if got := AnnotateRepo(in, nil); got.Content[0].Text != `{"a":1}` {
		t.Fatalf("got %q", got.Content[0].Text)
	}
}

// Host-bridge scenario: responses name the resolved repo, end to end through
// the callback server, and handlers that resolve nothing are unchanged.
func TestServer_AnnotatesRecordedRepo(t *testing.T) {
	srv := NewServer("127.0.0.1:0", "Bearer t", nil)
	srv.RegisterHandler("iris_resolves", HandlerFunc(func(ctx context.Context, in json.RawMessage) Response {
		verbs.RecordRepo(ctx, testRef)
		return TextResponse(`{"ok":true}`)
	}))
	srv.RegisterHandler("iris_plain", HandlerFunc(func(ctx context.Context, in json.RawMessage) Response {
		return TextResponse(`{"ok":true}`)
	}))
	srv.RegisterHandler("iris_json_error", HandlerFunc(func(ctx context.Context, in json.RawMessage) Response {
		verbs.RecordRepo(ctx, testRef)
		return ErrorResponse(`{"code":"field_not_local","field":"default_branch"}`)
	}))
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	call := func(tool string) string {
		req, _ := http.NewRequest("POST", srv.CallbackBaseURL()+"/mcp/"+tool, bytes.NewReader([]byte(`{"input":{}}`)))
		req.Header.Set("Authorization", "Bearer t")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var r Response
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return r.Content[0].Text
	}

	if got := call("iris_resolves"); !strings.Contains(got, `"repo":{"name":"acme/video-gen"`) {
		t.Fatalf("resolving handler not annotated: %s", got)
	}
	if got := call("iris_plain"); got != `{"ok":true}` {
		t.Fatalf("non-resolving handler changed: %s", got)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(call("iris_json_error")), &env); err != nil {
		t.Fatalf("JSON error envelope broken by annotation: %v", err)
	}
	if env["code"] != "field_not_local" || env["repo"] == nil {
		t.Fatalf("envelope: %v", env)
	}
}

package mcp

import (
	"encoding/json"
	"strings"

	"github.com/anutron/iris/internal/verbs"
)

// AnnotateRepo names ref in resp so a caller can see which repo a verb acted
// on. A JSON-object success body gains "repo" as its first key with every
// other byte unchanged; an error or non-JSON body gains a trailing
// "repo: <name> (<path>)" line. A nil ref leaves resp untouched.
func AnnotateRepo(resp Response, ref *verbs.RepoRef) Response {
	if ref == nil {
		return resp
	}
	if !resp.IsError && len(resp.Content) == 1 {
		if body, ok := injectRepoKey(resp.Content[0].Text, ref); ok {
			resp.Content = []ContentBlock{{Type: resp.Content[0].Type, Text: body}}
			return resp
		}
	}
	line := "repo: " + ref.String()
	content := append([]ContentBlock(nil), resp.Content...)
	if n := len(content); n > 0 && content[n-1].Type == "text" {
		content[n-1].Text = strings.TrimRight(content[n-1].Text, "\n") + "\n" + line
	} else {
		content = append(content, ContentBlock{Type: "text", Text: line})
	}
	resp.Content = content
	return resp
}

// injectRepoKey splices "repo" in as the first key of a JSON object body.
// It splices text rather than re-marshalling so the verb's own key order and
// indentation survive. Returns ok=false when body isn't a JSON object or
// already has a top-level "repo" key.
func injectRepoKey(body string, ref *verbs.RepoRef) (string, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &obj); err != nil || obj == nil {
		return "", false
	}
	if _, exists := obj["repo"]; exists {
		return "", false
	}
	refJSON, err := json.Marshal(ref)
	if err != nil {
		return "", false
	}
	open := strings.IndexByte(body, '{')
	head, rest := body[:open], body[open+1:]
	switch {
	case len(obj) == 0:
		return head + `{"repo":` + string(refJSON) + strings.TrimLeft(rest, " \t\r\n"), true
	case strings.HasPrefix(rest, "\n"):
		// Indented output (json.MarshalIndent with two spaces, as handlers use).
		return head + "{\n  \"repo\": " + string(refJSON) + "," + rest, true
	default:
		return head + `{"repo":` + string(refJSON) + "," + rest, true
	}
}

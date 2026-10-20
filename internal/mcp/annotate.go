package mcp

import (
	"encoding/json"
	"strings"

	"github.com/anutron/iris/internal/verbs"
)

// AnnotateRepo names ref in resp so a caller can see which repo a verb acted
// on. A body that parses as JSON is never appended to, so callers that
// decode it (e.g. set_local_config's {code, field, message, hint} error
// envelope) keep working: a JSON object without a top-level "repo" gains
// one as its first key, success or error alike, with every other byte
// unchanged; any other JSON is left alone. A plain-text body gains a final
// "repo: <name> (<path>)" line, with its existing text (check or build
// output) untouched. A nil ref leaves resp untouched.
func AnnotateRepo(resp Response, ref *verbs.RepoRef) Response {
	if ref == nil || len(resp.Content) == 0 {
		return resp
	}
	content := append([]ContentBlock(nil), resp.Content...)
	last := &content[len(content)-1]
	if last.Type != "text" {
		return resp
	}

	if json.Valid([]byte(last.Text)) {
		if body, ok := injectRepoKey(last.Text, ref); ok {
			last.Text = body
			resp.Content = content
		}
		return resp
	}

	sep := "\n"
	if strings.HasSuffix(last.Text, "\n") {
		sep = ""
	}
	last.Text += sep + "repo: " + ref.String()
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

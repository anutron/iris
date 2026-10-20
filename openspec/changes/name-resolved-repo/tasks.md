## 1. Repo naming in every response

- [x] 1.1 Write tests for origin-URL parsing, the request-scoped recorder, and server-side annotation of JSON, error and unresolved responses.
- [x] 1.2 Record resolutions in `Resolve`, `ResolveSelf` and `ResolvePath`, and annotate responses in `handleCallback`.

## 2. PR identity and expected_repo guard

- [x] 2.1 Write tests for merge and close: PR identity in the result, matching, mismatched and malformed `expected_repo`, and lookup failure with and without the guard.
- [x] 2.2 Implement the pre-flight `gh pr view` and the guard in both verbs, MCP handlers, CLI flags and tool schemas.

## 3. Documentation and delivery

- [x] 3.1 Document the cross-project `task_id` pattern, the `repo` field and `expected_repo` in the iris skill.
- [x] 3.2 File the unlocked post-merge checkout in `gh_pr_merge` as a todo note.
- [x] 3.3 Run `make test`, `go vet ./...` and `openspec validate --all --strict`.

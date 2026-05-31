## 1. Tests

- [x] 1.1 Fork origin → `gh pr create` argv includes `--repo <upstream>` and `--head <fork-owner>:<branch>`, no `--base`.
- [x] 1.2 Non-fork origin → argv keeps the existing `--base <default> --head <branch>` form (no `--repo`, no `owner:` head).
- [x] 1.3 Fork-detection failure → falls back to same-repo argv (does not abort).
- [x] 1.4 Default-branch refusal still fires before any gh invocation (fork or not).

## 2. Implementation

- [x] 2.1 Add a best-effort fork-detection helper: `gh repo view --json nameWithOwner,parent` in the source repo; parse `nameWithOwner` (fork owner) and `parent` (upstream owner/name). On error or no parent, report not-a-fork.
- [x] 2.2 In `GHPRCreate`, after the default-branch refusal, branch the argv: cross-fork (`--repo <upstream> --head <fork-owner>:<branch>`, no `--base`) when a fork is detected, else the existing same-repo form. Preserve title/body/draft handling and URL/number parsing.

## 3. Validation

- [x] 3.1 `gofmt`, `go vet`, `go test ./...` green (existing gh_pr_create tests still pass).
- [x] 3.2 `openspec validate gh-pr-create-cross-fork --strict` and `openspec validate --all --strict` clean.

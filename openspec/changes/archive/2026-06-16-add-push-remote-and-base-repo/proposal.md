## Why

`iris:push` is hardcoded to `origin` (`git push origin <branch>`), and `iris:gh_pr_create` can only open a same-repo PR on `origin` or — when `origin` is a fork — a *cross-fork* PR into the upstream parent (`<fork-owner>:<branch> → upstream`).

That leaves one common maintainer motion unreachable through iris: when `origin` is your fork (e.g. `anutron/argus`) but you have write access to the canonical upstream (`drn/argus`), you want to push the branch **directly to the upstream** and open a **same-repo PR there**. GitHub does not run CI/secrets on cross-fork PRs from a fork, so the auto-detected cross-fork PR is precisely the shape that won't run CI. The only way to get a CI-gated PR is for the branch to physically live on the upstream and the PR to be same-repo on the upstream.

Today an agent cannot do this through iris at all: `iris:push` won't target a remote other than `origin`, and `iris:gh_pr_create` has no way to say "open a same-repo PR on `drn/argus`" — its fork detection forces the CI-less cross-fork form.

## What Changes

- Add an optional `remote` parameter to `iris:push`. When provided, iris pushes to that named git remote instead of `origin`; when omitted, behavior is unchanged. The remote MUST be a remote already configured in the source repo (a name, never a URL) — iris validates it exists before pushing and never adds remotes or accepts ad-hoc URLs.
- Add an optional `base_repo` parameter to `iris:gh_pr_create`. When provided (e.g. `drn/argus`), iris opens a **same-repo PR on that repository** (`gh pr create --repo <base_repo> --head <effective-head>`, letting gh default the base to that repo's default branch) and **bypasses fork auto-detection entirely**. When omitted, behavior is unchanged (same-repo-on-origin, or cross-fork when origin is a fork).
- Existing safety rails are preserved and extended: the default-branch refusal and source-repo allowlist still apply; `remote` and `base_repo` are both rejected when they begin with `-` (flag-smuggling guard), and `base_repo` must be `owner/repo` shaped.
- The combined workflow becomes: `iris:push(remote="drn")` then `iris:gh_pr_create(base_repo="drn/argus")` — push to upstream, open a CI-gated same-repo PR there.
- Not breaking: both parameters are optional and default to today's `origin` / auto-detect behavior.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `iris-push`: the verb accepts an optional `remote` override; the push targets the named remote when present, else `origin`. The remote is validated to exist in the source repo.
- `iris-gh-pr-create`: the verb accepts an optional `base_repo`; when present, iris opens a same-repo PR on that repository and skips fork auto-detection.

## Impact

- `internal/verbs/push.go` — add `Remote` to `PushOptions` and `Remote` to `PushResult`; validate the remote, target it for push and rev-parse.
- `internal/verbs/gh_pr_create.go` — add `BaseRepo` to `GHPRCreateOptions`; validate it; when set, build the explicit-repo same-repo invocation and skip `detectForkUpstream`.
- `internal/mcp/handler_push.go` — accept and pass `remote`.
- `internal/mcp/handler_gh_pr_create.go` — accept and pass `base_repo`.
- `cmd/iris/push.go`, `cmd/iris/gh_pr_create.go` — add `--remote` / `--base-repo` CLI flags.
- MCP tool schemas + descriptions for `iris:push` and `iris:gh_pr_create` (the descriptions also gain explicit cross-fork-vs-push-to-upstream guidance, closing the discoverability gap that made this motion invisible to agents).
- Docs: `README.md` CLI table and a short workflow note; `claude/skills/iris/SKILL.md`.
- No data, migration, or cross-service impact.

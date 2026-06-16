## Context

iris is origin-centric by design (the "origin-first invariant"). `iris:push` runs `git push origin <effective-branch>`; `iris:gh_pr_create` either opens a same-repo PR on origin or, when origin is a detected fork, a cross-fork PR into the upstream parent. Both behaviors are correct for the fork-and-PR-to-your-own-copy flow.

They do not cover the maintainer flow where origin is a fork but the developer has write access to the upstream and wants a CI-gated PR. GitHub withholds CI/secrets from cross-fork PRs, so the only CI-gated option is: branch lives on the upstream, PR is same-repo on the upstream. This change adds the two minimal knobs that make that motion expressible without loosening any safety rail.

## Goals / Non-Goals

**Goals**

- Let `iris:push` target a non-origin **named** remote.
- Let `iris:gh_pr_create` open a same-repo PR on an explicitly named `base_repo`, bypassing fork auto-detection.
- Preserve every existing safety rail (default-branch refusal, allowlist, leading-dash guard).
- Keep both new parameters optional and fully backward-compatible.

**Non-Goals**

- iris does NOT add git remotes, accept remote URLs, or manage credentials. `remote` is a name that must already exist in the source repo.
- No change to the cross-fork auto-detection itself; `base_repo` simply takes precedence over it.
- No change to `iris:ship_feature` (it remains origin-targeted; a follow-up can compose these knobs if needed).

## Decisions

### `iris:push` — `remote` is a validated remote name, never a URL

Accepting a URL would turn iris into a vehicle for pushing to arbitrary hosts and undercut the "reuses existing git config, not a credential manager" invariant. Instead `remote` defaults to `origin`, and when provided iris runs `git remote get-url <remote>` first; a non-existent remote yields a clean structured error and no push. The leading-dash guard already used for `branch` applies to `remote`. The push and the resulting `git rev-parse <remote>/<effective-branch>` both target the named remote. `PushResult` gains a `remote` field so the caller can confirm where the branch landed.

### `iris:gh_pr_create` — `base_repo` opens a same-repo PR and suppresses fork detection

When `base_repo` is non-empty, iris builds `gh pr create --repo <base_repo> --head <effective-head> --title <T>` (plus `--body`/`--draft` when supplied) and **does not** call `detectForkUpstream`. `--base` is omitted so gh defaults it to `base_repo`'s own default branch (mirroring the cross-fork path, where origin's default branch may differ from the target's). The head is the plain effective branch — NOT fork-qualified — because the branch is expected to already exist on `base_repo` (the caller pushed it there, typically via `iris:push --remote`). `base_repo` is validated as `owner/repo` (exactly one `/`, non-empty halves) and rejected if it begins with `-`.

Precedence is explicit: `base_repo` > fork auto-detection > same-repo-on-origin. This means a maintainer who passes `base_repo` always gets the deterministic same-repo PR they asked for, never a surprise cross-fork PR.

### Discoverability

The MCP tool descriptions are rewritten to state the model out loud: `iris:push` notes it targets `origin` by default and that `remote` selects another configured remote; `iris:gh_pr_create` notes that origin-is-a-fork yields a cross-fork PR (no CI on the fork) and that `base_repo` opens a CI-gated same-repo PR on the named repo. This closes the gap that made the push-to-upstream motion invisible to agents.

## Risks / Trade-offs

- **Stale remote-tracking ref**: `git rev-parse <remote>/<branch>` relies on the push updating the remote-tracking ref. Named remotes added with `git remote add` carry the default fetch refspec, so the ref updates on push; if a remote were configured without it, rev-parse could read a stale/absent ref. Mitigation: the error path surfaces the rev-parse failure rather than reporting a false SHA.
- **`base_repo` write access**: opening a same-repo PR on `base_repo` requires the branch to exist there and the caller to have access. iris surfaces gh's error verbatim on failure; it does not pre-check access.

## Acceptance Criteria (Prove-It)

1. `iris:push` with `remote="second"` pushes the effective branch to the `second` remote (not origin) and reports `remote: "second"`.
2. `iris:push` with a `remote` that is not configured returns a structured error and performs no push.
3. `iris:push` with a `remote` beginning with `-` is rejected before git runs.
4. `iris:push` with no `remote` pushes to origin exactly as today (backward compatible).
5. `iris:gh_pr_create` with `base_repo="drn/argus"` runs `gh pr create --repo drn/argus --head <effective> --title ...`, omits `--base`, does NOT fork-qualify the head, and does NOT consult fork detection.
6. `iris:gh_pr_create` with `base_repo` set on a fork origin still produces the same-repo-on-base_repo invocation (base_repo wins over auto-detection).
7. `iris:gh_pr_create` with a `base_repo` beginning with `-` or not `owner/repo` shaped is rejected before gh runs.
8. `iris:gh_pr_create` with no `base_repo` preserves today's same-repo / cross-fork auto-detection behavior.
9. The default-branch refusal still applies to the effective head/branch under both new parameters.

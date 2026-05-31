## Why

`iris:gh_pr_create` can only open a PR within a single repo: it runs `gh pr create --base <default> --head <branch>` against `origin`. When `origin` is a **fork** and the real target is the upstream parent (the common `anutron/argus` fork → `drn/argus` upstream workflow), gh cannot express the `fork:branch → upstream:base` PR from that invocation and fails ("No commits between … / Head ref must be a branch"). So an agent in a sandbox can push its branch to the fork but cannot open the adoption PR — it has to hand a raw host `gh` command to a human. A dogfood-bootstrap worker hit exactly this.

## What Changes

- `iris:gh_pr_create` SHALL detect when the resolved source repo's `origin` is a fork (its GitHub repo has a parent) and, in that case, open a **cross-fork** PR against the upstream parent: `gh pr create --repo <upstream-owner>/<repo> --head <fork-owner>:<branch> …`, letting gh default the base to the upstream's default branch. When `origin` is not a fork, behavior is unchanged (same-repo PR with explicit `--base <default> --head <branch>`). Fork detection is best-effort: if it cannot be determined, iris falls back to the existing same-repo behavior.

## Capabilities

### Modified Capabilities

- `iris-gh-pr-create` — adds cross-fork (fork → upstream) PR support; same-repo behavior preserved.

## Impact

- `internal/verbs/gh_pr_create.go`: a fork-detection step (`gh repo view --json nameWithOwner,parent`) and branched argv assembly (cross-fork vs same-repo). The default-branch refusal on the head branch is unchanged.
- Tests: fork repo-view → asserts `--repo <upstream>` + `--head <fork-owner>:<branch>`; non-fork → asserts the existing `--base/--head` form; repo-view failure → falls back to same-repo.
- No change to other gh verbs or to the MCP/CLI surface (no new inputs).

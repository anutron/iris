## ADDED Requirements

### Requirement: gh_pr_merge never moves the host checkout

After a successful remote merge, `iris:gh_pr_merge` SHALL bring the local default branch up to date WITHOUT checking out any branch. It SHALL hold the per-source-repo lock and bound its fetch with the configured git transfer timeout. When the host is on the default branch it SHALL fast-forward in place. When the host is on any other branch (including the configured `dogfood_branch` or a detached HEAD) it SHALL leave the host checkout untouched and fast-forward only the local default ref, and only when that ref is an ancestor of `origin/<default>` and is not checked out in any worktree. Otherwise it SHALL append a warning and leave the ref. Sync failures SHALL NOT fail the (already completed) merge.

#### Scenario: Host on the dogfood branch stays put

- **WHEN** the host checkout is on `dev` and a PR merges to `main`
- **THEN** the host is still on `dev` with an unchanged HEAD and working tree, and local `main` equals the new `origin/main`

#### Scenario: Host on the default branch fast-forwards

- **WHEN** the host is on `main` and a PR merges
- **THEN** the host's `main` fast-forwards to `origin/main`

#### Scenario: Diverged local default is left alone

- **WHEN** local `main` has commits not on `origin/main` and the host is on `dev`
- **THEN** local `main` is unchanged, the host stays on `dev`, and a warning names the divergence

#### Scenario: Merge serializes with other source-repo verbs

- **WHEN** another verb holds the source-repo lock during the post-merge sync
- **THEN** the sync waits for the lock

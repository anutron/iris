## Why

Agents currently wait for the dogfood lease even when the deployed branch already contains their work, or the holder is deploying the same commit. Identifying the commit a claimant needs lets Iris satisfy those requests without unnecessary lease handoffs.

## What changes

- Add optional `sha` to `iris_dogfood_claim` and `--sha` to `iris dogfood claim`, resolving commit references such as `main` once on the host.
- Return `already_deployed` immediately when the dogfood branch contains the requested commit, whether the lease is free or busy. This grants no new lease and creates no queue entry.
- Persist each queued claimant's resolved commit; after a successful `set_dogfood`, remove every satisfied waiter and send a cancellable “your commit is now live” notice through the existing Argus pane path.
- Recheck containment before release hands the lease to a waiter, and during claim polling, so already satisfied requests do not acquire a lease.
- Preserve task-only behavior when no commit has been requested. Document that ordinary descendant-only deploys preserve satisfaction, while `--force` or external branch changes can invalidate it.
- Update the agent skill to pass the commit it needs and handle satisfaction without calling `set_dogfood` or releasing someone else's lease.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `iris-dogfood-lease`: Commit-aware claims, queue satisfaction after deployment and before handoff, persisted requested commits, and cancellable deployment notices.

## Impact

- Go lease state machine and persistence, lease verbs, and the `set_dogfood` success path.
- CLI claim flag, MCP claim schema/handler, verb/CLI/MCP tests, and `claude/skills/iris/SKILL.md`.
- Additive result status and optional persisted fields; existing task-only clients and state remain compatible. No new dependency.

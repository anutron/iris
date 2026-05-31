## Why

The dogfood bootstrap flow is self-inconsistent today. `iris:set_local_config` writes `.iris.local.toml` to the source repo root (by design — it is the per-developer overlay). But when the default branch does not yet gitignore that file, it shows as an untracked file, so the source repo's working tree is "dirty". The very next step, `iris:set_dogfood` → `iris:reload`, runs a pre-flight clean-tree check that then **refuses** ("working tree is dirty") over iris's *own* managed file. So an agent that follows the documented bootstrap cannot actually deploy until the gitignore line lands on the default branch — quietly re-introducing the "the adoption PR blocks dogfooding" coupling the design set out to avoid. A worker hit exactly this.

## What Changes

- iris's working-tree cleanliness check (`checkCleanTree`, shared by `iris:reload` pre-flight and `iris:publish`) SHALL treat `.iris.local.toml` as never-dirtying. It is iris's own managed per-developer overlay — written by `iris:set_local_config` and expected to sit at the source-repo root — so iris must not refuse to operate because of it. Any *other* uncommitted change still makes the tree dirty and still refuses.

## Capabilities

### Modified Capabilities

- `iris-reload` — the pre-flight clean-tree refusal ignores `.iris.local.toml`.
- `iris-publish` — the worktree/source clean-tree refusals ignore `.iris.local.toml`.

## Impact

- `internal/verbs/reload.go`: `checkCleanTree` filters `.iris.local.toml` from the porcelain status before deciding dirty (one shared helper; both `reload` and `publish` callers inherit it).
- Tests: clean-tree passes with only an untracked `.iris.local.toml`; still refuses a genuine dirty file.
- No change to `iris:status` reporting (`working_tree_clean` stays a faithful report of the raw tree state); this change is scoped to the *gating* checks that block reload/publish.

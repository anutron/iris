## Why

The agent-facing `iris` skill documents every `iris_*` verb, including the config-driven self-management verbs (`reload`, `publish`, `validate_config`, `set_local_config`). But it never teaches the agent the thing those verbs operate on: how to **author** `.iris.toml` and `.iris.local.toml`. An agent onboarding a repo to iris-managed reload, or fixing a config that `iris_validate_config`/`iris_reload` rejects, has the verbs but not the schema — so it guesses at field names, the shared-vs-local split, and the restart mechanisms, then loops against validation errors.

## What Changes

### New `claude/skills/iris/config-authoring.md`

- A progressive-disclosure reference the skill points to (keeps `SKILL.md` scannable). Covers: the two files and their overlay relationship; the field taxonomy (which field lives in which file) and the warning behavior when one is misplaced; the `[build]`, `[restart]`, and `[pre_flight]`/`[verify]`/`[post_merge]` blocks; the six restart mechanisms and the `exit_code`-is-self-only rule; the `iris_validate_config` (no-side-effect) and `iris_set_local_config` authoring loop; and worked example configs.

### Updated `claude/skills/iris/SKILL.md`

- The self-management section now lists `iris_set_local_config` alongside `iris_validate_config`, and a new "Authoring `.iris.toml` / `.iris.local.toml`" subsection gives a 30-second orientation and points the agent at `config-authoring.md` for the full schema.

## Capabilities

### Modified Capabilities

- `iris-agent-skill` — the skill gains a requirement that it document how to author both config files (schema, taxonomy, restart mechanisms, the validate/set loop, examples).

## Impact

- New `claude/skills/iris/config-authoring.md` (documentation; no Go code).
- `claude/skills/iris/SKILL.md`: self-management section updated; new authoring subsection.
- No changes to the daemon, verbs, MCP handlers, the snippet, or the installer scripts. The installer symlinks the whole `claude/skills/iris/` directory, so the new reference file ships automatically with no installer change.

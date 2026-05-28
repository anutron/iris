## Why

A Claude session spawned inside an argus worktree sees only the `mcp__argus__iris_*` tool names and one-line descriptions. It does not know it is in a sandbox, when to use iris vs. plain `Bash`, how iris composes with sibling plugins, or the workflows iris was built for — so it defaults to `Bash(git push)` / `Bash(gh pr create)`, which target host state the sandbox cannot reach. This change ships installable, runtime-gated documentation so any argus session discovers iris correctly.

## What Changes

### New `claude/skills/iris/SKILL.md`

- Agent-facing skill. Frontmatter `description` leads with the argus-awareness gate so the model only triggers it inside a sandbox.
- Body covers: the runtime gate (first section), what iris is, the core mental model, every `iris_*` tool with a "when to call" one-liner, decision rules, common Bash mistakes mapped to iris tools, composition with hera / plannotator-argus / argus core, and worked workflows.

### New `claude/snippets/iris.md`

- Optional always-in-context orientation fragment. First content line is the runtime gate.
- Short by design: gate + two-sentence orientation + top Bash→iris redirects + a pointer to load the full `iris` skill.
- Retains `tags` / `audience` frontmatter for compile-pipeline compatibility.

### Extended `setup.sh`

- New step: idempotently symlink `claude/skills/iris` → `~/.claude/skills/iris`; detect an existing correct symlink and skip.
- New step: offer (Y/n) to append the snippet **body** (frontmatter stripped) into `~/.claude/CLAUDE.md` between `# BEGIN IRIS (argus)` / `# END IRIS (argus)` markers; replace the block in place on re-run.
- Reports added / updated / unchanged / skipped per step. Reuses the existing `confirm()` and color helpers.

## Capabilities

### New Capabilities

- `iris-agent-skill` — the agent-facing skill, the orientation snippet, and the installer behavior that lands them in `~/.claude/`.

### Modified Capabilities

- (none) — `setup.sh` has no existing delta spec capability; its new behavior is captured under the new `iris-agent-skill` capability rather than a separate installer capability.

## Impact

- New `claude/skills/iris/SKILL.md` and `claude/snippets/iris.md` (documentation; no Go code).
- `setup.sh`: two new numbered steps + a frontmatter-stripping append helper.
- `README.md`: a short "Agent-facing discoverability" section pointing at the skill and the installer steps.
- No changes to the daemon, verbs, MCP handlers, or argus core.

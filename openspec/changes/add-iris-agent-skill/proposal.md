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

### New `install-claude-skills.sh` and `uninstall-claude-skills.sh`

- Dedicated, idempotent scripts (peers of `setup.sh`) that install / remove the agent-facing assets, sharing logic via `claude/lib-claude-assets.sh`.
- `install-claude-skills.sh` prompts (Y/n) **separately** to (a) symlink `claude/skills/iris` → `~/.claude/skills/iris` and (b) append the snippet **body** (frontmatter stripped) into `~/.claude/CLAUDE.md` between `# BEGIN IRIS (argus)` / `# END IRIS (argus)` markers (replaced in place on re-run). Declining the snippet prints its path. `--yes` accepts both.
- `uninstall-claude-skills.sh` prompts (Y/n) separately to remove the skill symlink (only if it points at this repo) and the CLAUDE.md block. No-op when nothing is present. `--yes` accepts both.

### Extended `setup.sh`

- After its daemon-setup steps, `setup.sh` prompts (Y/n) whether to install the Claude skills and delegates to `install-claude-skills.sh` (forwarding non-interactive mode). Declining points the user at the installer for later. No agent-asset logic lives in `setup.sh` itself.

## Capabilities

### New Capabilities

- `iris-agent-skill` — the agent-facing skill, the orientation snippet, and the install/uninstall behavior that lands them in (and removes them from) `~/.claude/`.

### Modified Capabilities

- (none) — the installer scripts have no existing delta spec capability; their behavior is captured under the new `iris-agent-skill` capability.

## Impact

- New `claude/skills/iris/SKILL.md` and `claude/snippets/iris.md` (documentation; no Go code).
- New `install-claude-skills.sh`, `uninstall-claude-skills.sh`, and `claude/lib-claude-assets.sh` (shared bash helpers).
- New `claude/install_test.sh` (installer + content-gate tests).
- `setup.sh`: one new Y/n prompt that delegates to `install-claude-skills.sh`.
- `README.md`: a short "Agent-facing discoverability" section pointing at the skill, snippet, and install/uninstall scripts.
- No changes to the daemon, verbs, MCP handlers, or argus core.

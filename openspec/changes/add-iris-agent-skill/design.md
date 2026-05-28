## Context

When a Claude session starts inside an argus worktree, the agent sees `mcp__argus__iris_*` tool names and their one-line descriptions — and nothing else. It does not know it is inside a sandbox, when to reach for iris vs. plain `Bash`, how iris composes with sibling plugins (hera, plannotator-argus, argus core), or the workflows iris was built for. The result is predictable: the agent reaches for `Bash(git push)` or `Bash(gh pr create)`, which target state outside its worktree (the canonical source repo, `origin`, GitHub) that the sandbox cannot reach.

The fix is **not** to extend argus core to inject CLAUDE.md fragments at worktree-create time. The fix is to ship installable skill + snippet artifacts in this repo so any user can `setup.sh` them into `~/.claude/` once and have every future argus session benefit. The artifacts self-gate at runtime so they stay inert in non-argus sessions.

This mirrors the precedent set by plannotator-argus, which solved the parallel discoverability problem with an installed snippet plus a PreToolUse guard hook. Iris reuses the snippet + gate idea but deliberately omits the guard (see Decision 4).

## Goals

- A fresh argus-sandboxed Claude session, asked "how do I ship this / open a PR / merge to master," reaches for the right `iris_*` tool without extra prompting.
- The skill is self-contained: every `iris_*` tool documented, decision rules, sibling composition, common Bash mistakes, and worked workflows.
- One idempotent `setup.sh` run installs the skill and (optionally) the orientation snippet for any developer who clones this repo.
- Artifacts stay inert outside argus sandboxes.

## Non-Goals

- No changes to argus core (no worktree-create-time snippet assembly).
- No top-level `CLAUDE.md` in this repo aimed at agents (that file is for plugin *developers*).
- No PreToolUse Bash guard hook (deferred — see Decision 4).
- Not scoped to one user; any developer who installs the plugin gets the same agent behavior.

## Decisions

### Decision 1: Docs ship in this repo, installed into `~/.claude/`, gated at runtime

Three artifacts live under a new top-level `claude/` directory:

- `claude/skills/iris/SKILL.md` — the agent-facing reference (primary surface).
- `claude/snippets/iris.md` — an optional always-in-context orientation fragment.
- `setup.sh` (extended) — symlinks the skill into `~/.claude/skills/iris` and offers to wire the snippet into `~/.claude/CLAUDE.md`.

The `claude/` directory is distinct from the repo's existing `.claude/` directory, which holds the *developer's* OpenSpec commands and skills. `claude/` is the agent-facing install payload; `.claude/` is plugin-developer tooling.

### Decision 2: Skill is the primary surface; snippet is optional orientation

The skill is what the model reaches for on demand — it can be long and complete. The snippet is for users who want orientation always loaded into context; it must stay short because it costs tokens on every turn. The snippet therefore carries only the gate, a two-sentence orientation, the handful of highest-value Bash→iris redirects, and a pointer to load the full `iris` skill.

### Decision 3: Gate on `~/.argus/worktrees/` cwd OR `ARGUS_TASK_ID`, in both frontmatter and body

The argus-awareness gate fires when EITHER the cwd is under `~/.argus/worktrees/` (equivalently `$HOME/.argus/worktrees/`) OR the `ARGUS_TASK_ID` env var is set. Outside both, the `iris_*` MCP tools are not registered in the session.

The gate appears in two places in the skill:

- The frontmatter `description` leads with it, so the model only *triggers* the skill inside an argus sandbox.
- The first body section repeats it as a runtime self-check, so once loaded the skill confirms the gate and, if it fails, tells the agent to use the `iris` CLI binary directly instead of the (unregistered) MCP tools.

The snippet's first content line is the gate, per the handoff: "If `ARGUS_TASK_ID` is unset and `$PWD` is not under `~/.argus/worktrees/`, ignore this section."

### Decision 4: No Bash guard (deferred)

plannotator-argus ships a PreToolUse guard because `plannotator <verb>` is an unambiguous 1:1 redirect to an MCP tool, and the direct call EPERMs. Iris is different: most `git` commands (local commits, branches, `status`, `diff`, `log`) are perfectly fine inside a worktree. Only a few operations — `git push`, `gh pr ...`, and anything that mutates the canonical source repo's checkout — need the host. A Bash guard cannot reliably tell source-repo intent from the command string alone, so it would either miss cases or deny legitimate local git. The skill's "common Bash mistakes" section does the teaching instead. If the skill proves insufficient in practice, a narrow guard (push + gh only) is a separate future change.

### Decision 5: `setup.sh` appends the snippet into `~/.claude/CLAUDE.md` between markers, with a Y/n prompt

Rather than assume every user has a snippet-compilation pipeline, `setup.sh` offers (Y/n) to append the snippet's **body** (YAML frontmatter stripped) into `~/.claude/CLAUDE.md` between `# BEGIN IRIS (argus)` / `# END IRIS (argus)` markers. Re-running replaces the marked block in place (idempotent) and reports added / updated / unchanged. Stripping the frontmatter keeps CLAUDE.md clean while the snippet file retains its `tags` / `audience` frontmatter so it still slots into a compile pipeline (e.g. Aaron's `claude-rules/snippets/`) for users who prefer that path.

### Decision 6: Skill install is a symlink, idempotent

`setup.sh` symlinks `claude/skills/iris` → `~/.claude/skills/iris` so live edits flow through. On re-run it detects an existing correct symlink and skips; it reports what changed. A pre-existing non-symlink at the target is surfaced as a warning, not clobbered.

## Risks / Trade-offs

- **Skill drift vs. tool descriptions.** The skill restates each tool's purpose; if the daemon's tool descriptions change, the skill can fall out of date. Mitigation: the skill documents *when to call* (intent), which changes less often than wording; the README and daemon descriptions remain the source of truth for exact arguments.
- **Snippet token cost.** Always-loaded orientation costs tokens every turn. Mitigation: the snippet is deliberately small and the gate lets the model skip it outside argus; it is opt-in via the Y/n prompt.
- **Gate false-negative.** If a future argus layout moves worktrees out of `~/.argus/worktrees/`, the cwd check breaks. Mitigation: the `ARGUS_TASK_ID` env check is an independent second signal.

## Alternatives considered

- **Extend argus core to inject CLAUDE.md fragments at worktree-create time.** Rejected by the handoff: docs should ship in the plugin repo and install into `~/.claude/`, not couple to argus core.
- **PreToolUse Bash guard (like plannotator-argus).** Deferred — see Decision 4. Too high a false-positive risk for `git`/`gh` whose intent isn't legible from the command string.
- **Symlink the snippet into a snippet-compilation dir.** Rejected as the default: not every user has such a pipeline. The marker-append into `~/.claude/CLAUDE.md` is generic; the snippet's frontmatter still supports the compile path for users who want it.

## Discovery findings

- Iris registers 21 MCP tools (`internal/daemon/run.go`), each `iris_*`-prefixed, resolving the canonical source repo from `task_id` via argus (`iris-host-bridge` spec). Non-self-hosting verbs require `task_id`; the five self-hosting verbs (`reload`, `validate_config`, `ls`, `status`, and `publish` from an argus worktree) can target iris itself.
- The repo already has `.claude/skills/` for *developer* tooling (OpenSpec commands) — confirming the agent-facing payload belongs under a separate `claude/` dir.
- The plannotator-argus precedent (`deploy/install.sh`, `deploy/plannotator-bash-guard.sh`) and the `airon-plannotator-in-argus` snippet establish the gate + redirect pattern this change reuses.
- `setup.sh` is already a structured, idempotent 5-step installer with `confirm()` and colored output helpers — the new skill/snippet steps slot in as additional numbered steps reusing those helpers.

## Acceptance criteria

### Skill content

- It should expose a frontmatter `description` that leads with the argus-awareness gate.
- It should, as its first body section, state the runtime gate and instruct the agent to use the `iris` CLI directly (not the MCP tools) when the gate fails.
- It should document every `iris_*` tool with a one-line "when to call" description.
- It should give decision rules for choosing among iris tools and between iris and plain Bash.
- It should list the common Bash commands that fail or are wrong in the sandbox and map each to its iris tool.
- It should explain composition with hera, plannotator-argus, and argus core.
- It should include at least two worked, end-to-end workflow examples as tool-call sequences.

### Snippet content

- It should carry the runtime gate as its first content line.
- It should retain `tags` and `audience` frontmatter for compile-pipeline compatibility.
- It should point the reader to the full `iris` skill.

### Installer behavior

- It should symlink `claude/skills/iris` into `~/.claude/skills/iris`.
- It should detect an existing correct symlink and skip it, reporting "already current."
- It should offer (Y/n) to append the snippet body into `~/.claude/CLAUDE.md` between `# BEGIN IRIS (argus)` / `# END IRIS (argus)` markers.
- It should strip the snippet's YAML frontmatter before appending to `~/.claude/CLAUDE.md`.
- It should replace the marked block in place on re-run rather than appending a duplicate.
- It should report what changed (added / updated / unchanged / skipped) for each step.

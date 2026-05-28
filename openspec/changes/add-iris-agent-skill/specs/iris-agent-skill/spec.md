# iris-agent-skill Specification

## ADDED Requirements

### Requirement: Agent-facing iris skill

The plugin SHALL ship a skill at `claude/skills/iris/SKILL.md` that orients a Claude session running inside an argus sandbox to iris's MCP tool surface. The skill's frontmatter `description` SHALL lead with the argus-awareness gate so the model only triggers the skill inside an argus sandbox. The skill body SHALL document, for iris's domain: what iris is, the core mental model (the worktree is local; the canonical source repo, `origin`, and GitHub live on the host and are reached only via iris), every `iris_*` MCP tool with a one-line "when to call" description, decision rules for choosing among the tools and between iris and plain `Bash`, the common `Bash` commands that fail or are wrong inside the sandbox mapped to their iris replacements, composition with the sibling plugins (hera, plannotator-argus, argus core), and at least two worked end-to-end workflow examples expressed as tool-call sequences.

#### Scenario: Description gates on argus awareness

- **WHEN** the skill's frontmatter `description` is read
- **THEN** it leads with the argus-awareness condition (cwd under `~/.argus/worktrees/` or `ARGUS_TASK_ID` set) so the model only reaches for the skill inside an argus sandbox

#### Scenario: First body section is the runtime gate

- **WHEN** the skill body is loaded
- **THEN** its first section instructs the agent to confirm the gate — cwd under `~/.argus/worktrees/` (equivalently `$HOME/.argus/worktrees/`) OR `ARGUS_TASK_ID` set — and, when the gate fails, states that the `iris_*` MCP tools are not registered in this session and the agent should use the `iris` CLI binary directly instead

#### Scenario: Every tool is documented

- **WHEN** the skill body's tool surface section is read
- **THEN** every `iris_*` MCP tool registered by the daemon appears with a one-line description framed around when to call it

#### Scenario: Common Bash mistakes are redirected

- **WHEN** the skill body's "common Bash mistakes" section is read
- **THEN** it lists `git push`, `gh pr ...`, and merging into the default branch (at minimum) as operations that target host state the sandbox cannot reach, and maps each to its iris tool (`iris_push`, `iris_gh_pr_*`, `iris_merge_to_master`), while noting that local git inside the worktree (commits, branches, `status`, `diff`, `log`) is fine

#### Scenario: Worked workflows are present

- **WHEN** the skill body's workflows section is read
- **THEN** it contains at least two end-to-end examples expressed as ordered `iris_*` tool-call sequences (for example: ship a finished task, and open a PR then mark it ready)

### Requirement: Optional orientation snippet

The plugin SHALL ship an optional CLAUDE.md snippet at `claude/snippets/iris.md` for users who want iris orientation always loaded into context. The snippet's first content line SHALL be the runtime gate. The snippet SHALL retain YAML frontmatter with `tags` and `audience` keys so it remains compatible with a snippet-compilation pipeline. The snippet SHALL stay brief — orientation plus the highest-value `Bash`→iris redirects — and SHALL point the reader to the full `iris` skill for the complete tool map.

#### Scenario: Gate is the first content line

- **WHEN** the snippet is read past its frontmatter
- **THEN** its first content line states that if `ARGUS_TASK_ID` is unset and `$PWD` is not under `~/.argus/worktrees/`, the section should be ignored

#### Scenario: Frontmatter is compile-pipeline friendly

- **WHEN** the snippet's frontmatter is read
- **THEN** it contains `tags` and `audience` keys in the same shape used by the existing snippet-compilation pipeline

#### Scenario: Snippet defers to the skill

- **WHEN** the snippet body is read
- **THEN** it directs the reader to load the full `iris` skill for the complete tool surface rather than duplicating it

### Requirement: Idempotent skill installation

A dedicated installer script `install-claude-skills.sh` SHALL install the agent-facing assets and SHALL be safe to re-run. It SHALL prompt (Y/n) before symlinking the skill, and SHALL prompt (Y/n) separately before wiring the snippet. A `--yes` flag SHALL accept both prompts non-interactively. When the skill prompt is accepted it SHALL symlink `claude/skills/iris` to `~/.claude/skills/iris`; on re-run it SHALL detect an existing correct symlink and report the skill as already current; a pre-existing path at the target that is not the expected symlink SHALL be surfaced as a warning and SHALL NOT be clobbered.

#### Scenario: Fresh install creates the symlink

- **WHEN** `install-claude-skills.sh` runs, the skill prompt is accepted, and `~/.claude/skills/iris` does not exist
- **THEN** it creates a symlink `~/.claude/skills/iris` → `<repo>/claude/skills/iris` and reports the link as created

#### Scenario: Re-run detects an existing symlink

- **WHEN** `install-claude-skills.sh` runs with the skill prompt accepted and `~/.claude/skills/iris` already points at `<repo>/claude/skills/iris`
- **THEN** it leaves the symlink in place and reports that the skill is already current

#### Scenario: Pre-existing non-symlink is not clobbered

- **WHEN** `install-claude-skills.sh` runs with the skill prompt accepted and `~/.claude/skills/iris` exists but is a regular directory or a symlink to a different target
- **THEN** it does NOT overwrite it; it warns naming the conflicting path and leaves it untouched

#### Scenario: Declining the skill prompt installs nothing

- **WHEN** `install-claude-skills.sh` runs and the user declines the skill prompt
- **THEN** no symlink is created at `~/.claude/skills/iris`

### Requirement: Snippet wiring into CLAUDE.md

`install-claude-skills.sh` SHALL offer, with its own Y/n prompt (separate from the skill prompt), to wire the orientation snippet into `~/.claude/CLAUDE.md`. When accepted, it SHALL append the snippet's body — with the YAML frontmatter stripped — between the markers `# BEGIN IRIS (argus)` and `# END IRIS (argus)`. On re-run it SHALL replace the content between the markers in place rather than appending a duplicate block, and SHALL report whether the block was added, updated, or left unchanged. When the prompt is declined, it SHALL leave `~/.claude/CLAUDE.md` unmodified and print the snippet's path so the user can wire it in manually.

#### Scenario: Accepting the prompt appends a marked block

- **GIVEN** `~/.claude/CLAUDE.md` contains no iris markers
- **WHEN** `install-claude-skills.sh` runs and the user accepts the snippet prompt
- **THEN** it appends the snippet body between `# BEGIN IRIS (argus)` and `# END IRIS (argus)`, with the YAML frontmatter removed, and reports the block as added

#### Scenario: Re-run replaces the marked block in place

- **GIVEN** `~/.claude/CLAUDE.md` already contains an iris marked block
- **WHEN** `install-claude-skills.sh` runs again and the user accepts the snippet prompt
- **THEN** it replaces only the content between the markers and reports the block as updated (or unchanged if identical), never appending a second block

#### Scenario: Declining prints the path

- **WHEN** `install-claude-skills.sh` runs and the user declines the snippet prompt
- **THEN** it does not modify `~/.claude/CLAUDE.md` and prints the absolute path to `claude/snippets/iris.md` so the user can wire it in manually

### Requirement: Agent-asset uninstallation

A dedicated `uninstall-claude-skills.sh` SHALL remove the agent-facing assets and SHALL be safe to re-run. It SHALL prompt (Y/n) before removing the skill symlink, and SHALL prompt (Y/n) separately before removing the snippet block; `--yes` SHALL accept both non-interactively. The skill symlink SHALL be removed only when it points at this repo; a symlink to a different target, a real directory, or an absent target SHALL be left untouched. The snippet block SHALL be removed by deleting the content between (and including) the `# BEGIN IRIS (argus)` / `# END IRIS (argus)` markers, and SHALL be a no-op when no block is present.

#### Scenario: Uninstall removes our own assets

- **GIVEN** the assets were installed by `install-claude-skills.sh`
- **WHEN** `uninstall-claude-skills.sh` runs and both prompts are accepted
- **THEN** the `~/.claude/skills/iris` symlink is removed and the iris marked block is removed from `~/.claude/CLAUDE.md`

#### Scenario: A foreign symlink is left alone

- **WHEN** `uninstall-claude-skills.sh` runs with the skill prompt accepted and `~/.claude/skills/iris` is a symlink to a target other than this repo
- **THEN** it does NOT remove it; it warns naming the target and leaves it untouched

#### Scenario: Uninstall on a clean target is a no-op

- **WHEN** `uninstall-claude-skills.sh` runs and neither the symlink nor the marked block is present
- **THEN** it exits successfully and reports there was nothing to remove

### Requirement: setup.sh delegates the skills install

The daemon installer `setup.sh` SHALL, after its daemon-setup steps, prompt (Y/n) whether to install the agent-facing Claude skills. When accepted it SHALL delegate to `install-claude-skills.sh` (forwarding non-interactive mode). When declined it SHALL skip the install and point the user at `install-claude-skills.sh` for later.

#### Scenario: Accepting delegates to the installer

- **WHEN** `setup.sh` finishes its daemon steps and the user accepts the skills prompt
- **THEN** it invokes `install-claude-skills.sh` (with `--yes` when `setup.sh` is running non-interactively)

#### Scenario: Declining skips the install

- **WHEN** `setup.sh` finishes its daemon steps and the user declines the skills prompt
- **THEN** it does not modify `~/.claude` and prints that the skills can be installed later via `install-claude-skills.sh`

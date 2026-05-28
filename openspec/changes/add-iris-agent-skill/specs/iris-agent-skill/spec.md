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

The installer (`setup.sh`) SHALL symlink `claude/skills/iris` to `~/.claude/skills/iris` and SHALL be safe to re-run. On re-run it SHALL detect an existing correct symlink and skip it, reporting that the skill is already current. A pre-existing path at the target that is not the expected symlink SHALL be surfaced as a warning and SHALL NOT be clobbered.

#### Scenario: Fresh install creates the symlink

- **WHEN** `setup.sh` runs and `~/.claude/skills/iris` does not exist
- **THEN** it creates a symlink `~/.claude/skills/iris` → `<repo>/claude/skills/iris` and reports the link as created

#### Scenario: Re-run detects an existing symlink

- **WHEN** `setup.sh` runs and `~/.claude/skills/iris` already points at `<repo>/claude/skills/iris`
- **THEN** it leaves the symlink in place and reports that the skill is already current

#### Scenario: Pre-existing non-symlink is not clobbered

- **WHEN** `setup.sh` runs and `~/.claude/skills/iris` exists but is a regular directory or a symlink to a different target
- **THEN** it does NOT overwrite it; it warns naming the conflicting path and leaves it untouched

### Requirement: Snippet wiring into CLAUDE.md

The installer SHALL offer, with a Y/n prompt, to wire the orientation snippet into `~/.claude/CLAUDE.md`. When accepted, it SHALL append the snippet's body — with the YAML frontmatter stripped — between the markers `# BEGIN IRIS (argus)` and `# END IRIS (argus)`. On re-run it SHALL replace the content between the markers in place rather than appending a duplicate block, and SHALL report whether the block was added, updated, or left unchanged. When the prompt is declined, it SHALL print the snippet's path so the user can wire it in manually.

#### Scenario: Accepting the prompt appends a marked block

- **GIVEN** `~/.claude/CLAUDE.md` contains no iris markers
- **WHEN** `setup.sh` runs and the user accepts the snippet prompt
- **THEN** it appends the snippet body between `# BEGIN IRIS (argus)` and `# END IRIS (argus)`, with the YAML frontmatter removed, and reports the block as added

#### Scenario: Re-run replaces the marked block in place

- **GIVEN** `~/.claude/CLAUDE.md` already contains an iris marked block
- **WHEN** `setup.sh` runs again and the user accepts the snippet prompt
- **THEN** it replaces only the content between the markers and reports the block as updated (or unchanged if identical), never appending a second block

#### Scenario: Declining prints the path

- **WHEN** `setup.sh` runs and the user declines the snippet prompt
- **THEN** it does not modify `~/.claude/CLAUDE.md` and prints the absolute path to `claude/snippets/iris.md` so the user can wire it in manually

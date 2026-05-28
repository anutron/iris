**Design doc:** `openspec/changes/add-iris-agent-skill/design.md`

## 1. Tests

- [ ] 1.1 Write `claude/install_test.sh`: a bash test that runs the installer's agent-asset steps against a throwaway `HOME` and asserts the behaviors from the `iris-agent-skill` deltas (symlink created; re-run idempotent; pre-existing non-symlink not clobbered; CLAUDE.md marker block added; re-run replaces in place, no duplicate; frontmatter stripped; decline prints the path). It SHALL fail until Stages 2-3 land.
- [ ] 1.2 Write a content-gate check (in `claude/install_test.sh` or alongside) asserting: `claude/skills/iris/SKILL.md` frontmatter `description` mentions the argus gate; the skill body's first section names `~/.argus/worktrees/` and `ARGUS_TASK_ID`; the snippet's first content line is the gate; the snippet frontmatter has `tags` and `audience`.
- [ ] 1.3 Confirm 1.1 and 1.2 fail against the current tree (Prove-It).

## 2. Skill + snippet content

**Depends on:** Stage 1

- [ ] 2.1 Write `claude/skills/iris/SKILL.md` frontmatter: `name: iris`; `description` leading with the argus-awareness gate and the trigger ("use when you need to push, open/merge PRs, merge to the default branch, or otherwise act on the canonical source repo from inside an argus sandbox").
- [ ] 2.2 Write the skill body section 1 — the runtime gate: confirm cwd under `~/.argus/worktrees/` OR `ARGUS_TASK_ID` set; if neither, state the `iris_*` MCP tools are not registered here and to use the `iris` CLI binary directly, then stop.
- [ ] 2.3 Write "What iris is" + "Core mental model" (worktree is local; source repo / `origin` / GitHub live on the host; iris reaches them, resolving from `task_id`).
- [ ] 2.4 Write the tool-surface section: every `iris_*` tool (cross-check against `internal/daemon/run.go`) grouped as Ship / PR lifecycle / Branch & history / Build / Self-management, each with a "when to call" one-liner.
- [ ] 2.5 Write "When to use what" decision rules and the "Common Bash mistakes" table (`git push`→`iris_push`, `gh pr ...`→`iris_gh_pr_*`, merge to default→`iris_merge_to_master`, `git tag && push --tags`→`iris_tag`, source-repo checkout→`iris_checkout`/`iris_branch_create`), noting local git in the worktree is fine.
- [ ] 2.6 Write "Composition with sibling plugins": hera (orchestration — worker finishes, iris ships its branch), plannotator-argus (review UI), argus core (lifecycle — `task_complete` vs the composite `iris_complete_task`).
- [ ] 2.7 Write 2-3 worked workflows as ordered tool-call sequences: (A) ship a finished task (`run_build` → `push` → `gh_pr_create` → poll `gh_pr_view` → `gh_pr_merge`, or `complete_task`); (B) open a PR for review then ready it (`push` → `gh_pr_create` draft → `gh_pr_ready`); (C) cherry-pick a hotfix / recover a stuck source repo.
- [ ] 2.8 Write the "Gotchas" closer (`task_id` required, `argus/` branch prefix, iris never deletes the worktree, `complete_task` is the composite).
- [ ] 2.9 Write `claude/snippets/iris.md`: frontmatter (`tags: [iris, argus]`, `audience: [shared]`); first content line is the gate; two-sentence orientation; top Bash→iris redirects; "load the `iris` skill for the full tool map."

## 3. Installer

**Depends on:** Stage 1

- [ ] 3.1 Add a `SKILL_SRC="${SCRIPT_DIR}/claude/skills/iris"`, `SKILL_DEST="${HOME}/.claude/skills/iris"`, `SNIPPET_SRC="${SCRIPT_DIR}/claude/snippets/iris.md"`, `CLAUDE_MD="${HOME}/.claude/CLAUDE.md"` config block to `setup.sh`.
- [ ] 3.2 Add a `--skill-only` flag that runs only the new agent-asset steps (skill symlink + snippet offer) and exits — used by the test and by users who want the docs without the daemon.
- [ ] 3.3 Implement the skill-symlink step: create `~/.claude/skills/` if missing; if `SKILL_DEST` is already the correct symlink, report "already current"; if it is a different file/dir/symlink, warn and skip without clobbering; else create the symlink and report it.
- [ ] 3.4 Implement a `strip_frontmatter()` helper that emits a file's body with a leading `---`…`---` YAML block removed.
- [ ] 3.5 Implement the snippet step: `confirm()` (Y/n) to append into `~/.claude/CLAUDE.md`; on accept, write the stripped snippet body between `# BEGIN IRIS (argus)` / `# END IRIS (argus)`, replacing an existing marked block in place (no duplicate) and reporting added/updated/unchanged; on decline, print the absolute snippet path.
- [ ] 3.6 Wire both steps into the normal (non-`--skill-only`) flow as new numbered steps; renumber the step headers and update the trailing "Setup complete" guidance to mention the skill.
- [ ] 3.7 Run `bash claude/install_test.sh`; iterate until green.

## 4. Docs + validation

**Depends on:** Stage 2, Stage 3

- [ ] 4.1 Add a short "Agent-facing discoverability" section to `README.md` pointing at `claude/skills/iris/`, the snippet, and the `setup.sh` (incl. `--skill-only`) install path.
- [ ] 4.2 `openspec validate add-iris-agent-skill --strict` clean.
- [ ] 4.3 `openspec validate --all --strict` clean.
- [ ] 4.4 Run `./setup.sh --skill-only` locally; verify `~/.claude/skills/iris` resolves to the repo and the CLAUDE.md marker block is correct.

**Design doc:** `openspec/changes/add-iris-agent-skill/design.md`

## 1. Tests

- [x] 1.1 Write `claude/install_test.sh`: a bash test that runs `install-claude-skills.sh` / `uninstall-claude-skills.sh` against a throwaway `HOME` and asserts the `iris-agent-skill` deltas (symlink create/idempotent/non-symlink-not-clobbered; per-action Y/n incl. decline-snippet and decline-both; CLAUDE.md block add/replace-in-place/frontmatter-stripped/decline-prints-path; uninstall removes our assets, leaves a foreign symlink, no-ops clean). It SHALL fail until Stages 2-3 land.
- [x] 1.2 Write a content-gate check (in `claude/install_test.sh` or alongside) asserting: `claude/skills/iris/SKILL.md` frontmatter `description` mentions the argus gate; the skill body's first section names `~/.argus/worktrees/` and `ARGUS_TASK_ID`; the snippet's first content line is the gate; the snippet frontmatter has `tags` and `audience`.
- [x] 1.3 Confirm 1.1 and 1.2 fail against the current tree (Prove-It).

## 2. Skill + snippet content

**Depends on:** Stage 1

- [x] 2.1 Write `claude/skills/iris/SKILL.md` frontmatter: `name: iris`; `description` leading with the argus-awareness gate and the trigger ("use when you need to push, open/merge PRs, merge to the default branch, or otherwise act on the canonical source repo from inside an argus sandbox").
- [x] 2.2 Write the skill body section 1 — the runtime gate: confirm cwd under `~/.argus/worktrees/` OR `ARGUS_TASK_ID` set; if neither, state the `iris_*` MCP tools are not registered here and to use the `iris` CLI binary directly, then stop.
- [x] 2.3 Write "What iris is" + "Core mental model" (worktree is local; source repo / `origin` / GitHub live on the host; iris reaches them, resolving from `task_id`).
- [x] 2.4 Write the tool-surface section: every `iris_*` tool (cross-check against `internal/daemon/run.go`) grouped as Ship / PR lifecycle / Branch & history / Build / Self-management, each with a "when to call" one-liner.
- [x] 2.5 Write "When to use what" decision rules and the "Common Bash mistakes" table (`git push`→`iris_push`, `gh pr ...`→`iris_gh_pr_*`, merge to default→`iris_merge_to_master`, `git tag && push --tags`→`iris_tag`, source-repo checkout→`iris_checkout`/`iris_branch_create`), noting local git in the worktree is fine.
- [x] 2.6 Write "Composition with sibling plugins": hera (orchestration — worker finishes, iris ships its branch), plannotator-argus (review UI), argus core (lifecycle — `task_complete` vs the composite `iris_complete_task`).
- [x] 2.7 Write 2-3 worked workflows as ordered tool-call sequences: (A) ship a finished task (`run_build` → `push` → `gh_pr_create` → poll `gh_pr_view` → `gh_pr_merge`, or `complete_task`); (B) open a PR for review then ready it (`push` → `gh_pr_create` draft → `gh_pr_ready`); (C) cherry-pick a hotfix / recover a stuck source repo.
- [x] 2.8 Write the "Gotchas" closer (`task_id` required, `argus/` branch prefix, iris never deletes the worktree, `complete_task` is the composite).
- [x] 2.9 Write `claude/snippets/iris.md`: frontmatter (`tags: [iris, argus]`, `audience: [shared]`); first content line is the gate; two-sentence orientation; top Bash→iris redirects; "load the `iris` skill for the full tool map."

## 3. Installer

**Depends on:** Stage 1

- [x] 3.1 Write `claude/lib-claude-assets.sh` (sourced, not executed): constants (`SKILL_SRC`/`SKILL_DEST`/`SNIPPET_SRC`/`CLAUDE_MD`/markers computed relative to the lib), color helpers, `confirm()` honoring `ASSUME_YES`, `strip_frontmatter()`, `link_skill`/`unlink_skill`, `append_snippet`/`remove_snippet_block`.
- [x] 3.2 Write `install-claude-skills.sh`: source the lib; parse `--yes`; prompt (Y/n) separately for `link_skill` and `append_snippet`; decline-snippet prints the path.
- [x] 3.3 Write `uninstall-claude-skills.sh`: source the lib; parse `--yes`; prompt (Y/n) separately for `unlink_skill` (repo-only) and `remove_snippet_block`.
- [x] 3.4 `link_skill`: idempotent symlink; report already-current; warn + no-clobber on any other pre-existing path. `unlink_skill`: remove only when it points at this repo.
- [x] 3.5 `append_snippet`: write stripped body between markers, replace in place on re-run (no duplicate), report added/updated/unchanged. `remove_snippet_block`: delete the marked block; no-op when absent.
- [x] 3.6 Extend `setup.sh`: after daemon setup, prompt (Y/n) to install the skills and delegate to `install-claude-skills.sh` (forward `--yes` when non-interactive); decline points at the installer. Update the usage comment.
- [x] 3.7 `chmod +x` the scripts; run `bash claude/install_test.sh`; iterate until green.

## 4. Docs + validation

**Depends on:** Stage 2, Stage 3

- [x] 4.1 Add a short "Agent-facing discoverability" section to `README.md` pointing at `claude/skills/iris/`, the snippet, and `install-claude-skills.sh` / `uninstall-claude-skills.sh`.
- [x] 4.2 `openspec validate add-iris-agent-skill --strict` clean.
- [x] 4.3 `openspec validate --all --strict` clean.
- [x] 4.4 `bash claude/install_test.sh` green (exercises install + uninstall against a throwaway HOME — the host `~/.claude` run is left to the user, since it's outside the argus sandbox).

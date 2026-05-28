#!/usr/bin/env bash
# install_test.sh — exercises install-claude-skills.sh / uninstall-claude-skills.sh
# against a throwaway HOME, plus content-gate checks on the shipped skill and
# snippet.
#
# Covers the iris-agent-skill deltas:
#   install: skill symlink (create / re-run-idempotent / non-symlink-not-clobbered),
#            snippet wiring (accept appends one marked block, re-run replaces in
#            place, frontmatter stripped), per-action Y/n (decline snippet, decline
#            both)
#   uninstall: removes our symlink + block; leaves a foreign symlink alone;
#              idempotent on a clean HOME
#   content: skill description + body name the argus gate; snippet first content
#            line is the gate; snippet frontmatter has tags/audience
#
# Each Y/n prompt is fed via stdin in order (symlink prompt, then snippet prompt).
# Usage: bash claude/install_test.sh   (exit 0 = all pass)

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL="${REPO_ROOT}/install-claude-skills.sh"
UNINSTALL="${REPO_ROOT}/uninstall-claude-skills.sh"
SKILL_SRC="${REPO_ROOT}/claude/skills/iris"
SNIPPET_SRC="${REPO_ROOT}/claude/snippets/iris.md"

PASS=0
FAIL=0
ok()  { printf '  \033[32mok\033[0m   %s\n' "$1"; PASS=$((PASS+1)); }
bad() { printf '  \033[31mFAIL\033[0m %s\n' "$1"; FAIL=$((FAIL+1)); }

# run <script> <home> <stdin> [args...] -> stdout+stderr in $OUT, rc in $RC
run() {
  local script="$1" home="$2" stdin_data="$3"; shift 3
  OUT="$(printf '%s' "$stdin_data" | HOME="$home" bash "$script" "$@" 2>&1)"
  RC=$?
}

mktmphome() { local d; d="$(mktemp -d)"; mkdir -p "$d/.claude"; printf '%s' "$d"; }
nblocks()   { grep -c '# BEGIN IRIS (argus)' "$1" 2>/dev/null || echo 0; }

# --- install: skill symlink -------------------------------------------------
test_symlink() {
  local h; h="$(mktmphome)"
  run "$INSTALL" "$h" "" --yes
  if [[ -L "$h/.claude/skills/iris" && "$(readlink "$h/.claude/skills/iris")" == "$SKILL_SRC" ]]; then
    ok "install --yes symlinks ~/.claude/skills/iris -> repo"
  else
    bad "install did not create the skill symlink (rc=$RC; out: $OUT)"
  fi

  run "$INSTALL" "$h" "" --yes
  if [[ -L "$h/.claude/skills/iris" ]] && grep -qi "already current" <<<"$OUT"; then
    ok "re-run leaves symlink and reports already current"
  else
    bad "re-run not idempotent / no 'already current' (out: $OUT)"
  fi
  rm -rf "$h"
}

test_no_clobber() {
  local h; h="$(mktmphome)"
  mkdir -p "$h/.claude/skills/iris"; touch "$h/.claude/skills/iris/sentinel"
  run "$INSTALL" "$h" "" --yes
  if [[ -d "$h/.claude/skills/iris" && ! -L "$h/.claude/skills/iris" && -f "$h/.claude/skills/iris/sentinel" ]] \
     && grep -qiE "warn|untouched|exist" <<<"$OUT"; then
    ok "pre-existing non-symlink left untouched with a warning"
  else
    bad "pre-existing dir was clobbered or no warning (out: $OUT)"
  fi
  rm -rf "$h"
}

# --- install: snippet wiring ------------------------------------------------
test_snippet_accept() {
  local h; h="$(mktmphome)"
  run "$INSTALL" "$h" "" --yes
  local md="$h/.claude/CLAUDE.md"
  [[ "$(nblocks "$md")" == "1" ]] && grep -q '# END IRIS (argus)' "$md" 2>/dev/null \
    && ok "accept appends exactly one marked block" || bad "marked block missing/duplicated"
  [[ -f "$md" ]] && ! grep -q '^audience:' "$md" \
    && ok "snippet frontmatter stripped from CLAUDE.md" || bad "frontmatter leaked into CLAUDE.md"
  [[ -f "$md" ]] && grep -qi "ignore this section" "$md" \
    && ok "snippet body present in CLAUDE.md" || bad "snippet body not found in CLAUDE.md"
  run "$INSTALL" "$h" "" --yes
  [[ "$(nblocks "$md")" == "1" ]] \
    && ok "re-run replaces block in place (no duplicate)" || bad "re-run duplicated the block"
  rm -rf "$h"
}

# decline the snippet prompt only: y to symlink, n to snippet
test_decline_snippet() {
  local h; h="$(mktmphome)"
  run "$INSTALL" "$h" $'y\nn\n'
  local md="$h/.claude/CLAUDE.md"
  if [[ -L "$h/.claude/skills/iris" ]] && { [[ ! -f "$md" ]] || ! grep -q '# BEGIN IRIS' "$md"; }; then
    ok "decline snippet: symlink made, CLAUDE.md untouched"
  else
    bad "decline snippet did the wrong thing (out: $OUT)"
  fi
  grep -qF "claude/snippets/iris.md" <<<"$OUT" \
    && ok "decline snippet prints the path" || bad "decline snippet did not print path (out: $OUT)"
  rm -rf "$h"
}

# decline both prompts: n, n
test_decline_both() {
  local h; h="$(mktmphome)"
  run "$INSTALL" "$h" $'n\nn\n'
  local md="$h/.claude/CLAUDE.md"
  if [[ ! -e "$h/.claude/skills/iris" ]] && { [[ ! -f "$md" ]] || ! grep -q '# BEGIN IRIS' "$md"; }; then
    ok "decline both: nothing installed"
  else
    bad "decline both still installed something (out: $OUT)"
  fi
  rm -rf "$h"
}

# --- uninstall --------------------------------------------------------------
test_uninstall() {
  local h; h="$(mktmphome)"
  run "$INSTALL" "$h" "" --yes
  run "$UNINSTALL" "$h" "" --yes
  local md="$h/.claude/CLAUDE.md"
  if [[ ! -e "$h/.claude/skills/iris" ]]; then
    ok "uninstall removes the skill symlink"
  else
    bad "uninstall left the skill symlink (out: $OUT)"
  fi
  if [[ ! -f "$md" ]] || ! grep -q '# BEGIN IRIS' "$md"; then
    ok "uninstall removes the CLAUDE.md block"
  else
    bad "uninstall left the CLAUDE.md block"
  fi
  rm -rf "$h"
}

test_uninstall_foreign_symlink() {
  local h; h="$(mktmphome)"; local other; other="$(mktemp -d)"
  mkdir -p "$h/.claude/skills"; ln -s "$other" "$h/.claude/skills/iris"
  run "$UNINSTALL" "$h" "" --yes
  if [[ -L "$h/.claude/skills/iris" && "$(readlink "$h/.claude/skills/iris")" == "$other" ]] \
     && grep -qiE "not this repo|untouched" <<<"$OUT"; then
    ok "uninstall leaves a foreign symlink alone"
  else
    bad "uninstall removed a foreign symlink (out: $OUT)"
  fi
  rm -rf "$h" "$other"
}

test_uninstall_clean() {
  local h; h="$(mktmphome)"
  run "$UNINSTALL" "$h" "" --yes
  [[ "$RC" -eq 0 ]] && ok "uninstall on a clean HOME is a no-op" || bad "uninstall on clean HOME failed (rc=$RC)"
  rm -rf "$h"
}

# --- content gates ----------------------------------------------------------
test_content() {
  local skill="${SKILL_SRC}/SKILL.md"
  if [[ -f "$skill" ]]; then
    local fm; fm="$(awk 'NR==1 && /^---/{f=1; next} f && /^---/{exit} f{print}' "$skill")"
    grep -q 'description:' <<<"$fm" && grep -q '\.argus/worktrees' <<<"$fm" && grep -q 'ARGUS_TASK_ID' <<<"$fm" \
      && ok "skill description names the argus gate" || bad "skill description does not name the gate"
    grep -q '\.argus/worktrees' "$skill" && grep -q 'ARGUS_TASK_ID' "$skill" \
      && ok "skill body references both gate signals" || bad "skill body missing a gate signal"
  else
    bad "skill file missing: $skill"
  fi
  if [[ -f "$SNIPPET_SRC" ]]; then
    local first; first="$(awk 'BEGIN{fm=0} /^---[[:space:]]*$/{fm++; next} fm>=2 && NF{print; exit}' "$SNIPPET_SRC")"
    grep -qi "ignore this section" <<<"$first" \
      && ok "snippet first content line is the gate" || bad "snippet first line is not the gate (got: $first)"
    grep -q '^tags:' "$SNIPPET_SRC" && grep -q '^audience:' "$SNIPPET_SRC" \
      && ok "snippet frontmatter has tags and audience" || bad "snippet frontmatter missing tags/audience"
  else
    bad "snippet file missing: $SNIPPET_SRC"
  fi
}

echo "install_test.sh"
echo "install — skill symlink:"; test_symlink; test_no_clobber
echo "install — snippet wiring:"; test_snippet_accept; test_decline_snippet; test_decline_both
echo "uninstall:"; test_uninstall; test_uninstall_foreign_symlink; test_uninstall_clean
echo "content gates:"; test_content

echo
echo "passed: $PASS  failed: $FAIL"
[[ "$FAIL" -eq 0 ]]

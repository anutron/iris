#!/usr/bin/env bash
# install_test.sh — exercises setup.sh's agent-asset install path against a
# throwaway HOME, plus content-gate checks on the shipped skill and snippet.
#
# Covers the iris-agent-skill deltas:
#   - skill symlink: create / re-run-idempotent / pre-existing-non-symlink-not-clobbered
#   - snippet wiring: accept appends a marked block, re-run replaces in place
#     (no duplicate), frontmatter stripped, decline prints the path
#   - content gates: skill description + first body section name the argus gate;
#     snippet first content line is the gate; snippet frontmatter has tags/audience
#
# Runs nothing that needs argus, go, or launchd: setup.sh --skill-only must
# short-circuit to just the agent-asset steps.
#
# Usage: bash claude/install_test.sh   (exit 0 = all pass)

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SETUP="${REPO_ROOT}/setup.sh"
SKILL_SRC="${REPO_ROOT}/claude/skills/iris"
SNIPPET_SRC="${REPO_ROOT}/claude/snippets/iris.md"

PASS=0
FAIL=0

ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; PASS=$((PASS+1)); }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; FAIL=$((FAIL+1)); }

# run_setup <home> [stdin] [extra-args...] -> stdout+stderr captured to $OUT
run_setup() {
  local home="$1"; shift
  local stdin_data="$1"; shift
  OUT="$(printf '%s' "$stdin_data" | HOME="$home" bash "$SETUP" --skill-only "$@" 2>&1)"
  RC=$?
}

mktmphome() {
  local d; d="$(mktemp -d)"; mkdir -p "$d/.claude"; printf '%s' "$d"
}

# --- 1. fresh install creates the symlink -----------------------------------
section_symlink() {
  local h; h="$(mktmphome)"
  run_setup "$h" "" --yes
  if [[ -L "$h/.claude/skills/iris" && "$(readlink "$h/.claude/skills/iris")" == "$SKILL_SRC" ]]; then
    ok "fresh install symlinks ~/.claude/skills/iris -> repo"
  else
    bad "fresh install did not create the skill symlink (rc=$RC)"
  fi

  # 2. re-run is idempotent and reports current
  run_setup "$h" "" --yes
  if [[ -L "$h/.claude/skills/iris" ]] && grep -qi "already current" <<<"$OUT"; then
    ok "re-run leaves symlink and reports already current"
  else
    bad "re-run not idempotent / no 'already current' (out: $OUT)"
  fi

  rm -rf "$h"
}

# 3. pre-existing non-symlink is not clobbered
section_no_clobber() {
  local h; h="$(mktmphome)"
  mkdir -p "$h/.claude/skills/iris"
  touch "$h/.claude/skills/iris/sentinel"
  run_setup "$h" "" --yes
  if [[ -d "$h/.claude/skills/iris" && ! -L "$h/.claude/skills/iris" && -f "$h/.claude/skills/iris/sentinel" ]] \
     && grep -qiE "warn|skip|exist" <<<"$OUT"; then
    ok "pre-existing non-symlink left untouched with a warning"
  else
    bad "pre-existing dir was clobbered or no warning (out: $OUT)"
  fi
  rm -rf "$h"
}

# --- snippet wiring ---------------------------------------------------------
section_snippet_accept() {
  local h; h="$(mktmphome)"
  run_setup "$h" "" --yes
  local md="$h/.claude/CLAUDE.md"
  local begins; begins="$(grep -c '# BEGIN IRIS (argus)' "$md" 2>/dev/null || echo 0)"
  if [[ "$begins" == "1" ]] && grep -q '# END IRIS (argus)' "$md" 2>/dev/null; then
    ok "accept appends exactly one marked block to CLAUDE.md"
  else
    bad "marked block missing or duplicated (begins=$begins)"
  fi
  # frontmatter stripped: the snippet's 'audience:' frontmatter key must not appear
  if [[ -f "$md" ]] && ! grep -q '^audience:' "$md"; then
    ok "snippet frontmatter stripped from CLAUDE.md"
  else
    bad "snippet frontmatter leaked into CLAUDE.md"
  fi
  # body content present
  if [[ -f "$md" ]] && grep -qi "ignore this section" "$md"; then
    ok "snippet body (gate line) present in CLAUDE.md"
  else
    bad "snippet body not found in CLAUDE.md"
  fi

  # re-run replaces in place, no duplicate
  run_setup "$h" "" --yes
  local begins2; begins2="$(grep -c '# BEGIN IRIS (argus)' "$md" 2>/dev/null || echo 0)"
  if [[ "$begins2" == "1" ]]; then
    ok "re-run replaces marked block in place (no duplicate)"
  else
    bad "re-run duplicated the marked block (begins=$begins2)"
  fi
  rm -rf "$h"
}

section_snippet_decline() {
  local h; h="$(mktmphome)"
  run_setup "$h" $'n\n'   # decline the snippet prompt
  local md="$h/.claude/CLAUDE.md"
  if [[ ! -f "$md" ]] || ! grep -q '# BEGIN IRIS (argus)' "$md" 2>/dev/null; then
    ok "decline leaves CLAUDE.md unmodified"
  else
    bad "decline still wrote to CLAUDE.md"
  fi
  if grep -qF "claude/snippets/iris.md" <<<"$OUT"; then
    ok "decline prints the snippet path"
  else
    bad "decline did not print the snippet path (out: $OUT)"
  fi
  rm -rf "$h"
}

# --- content gates ----------------------------------------------------------
section_content() {
  local skill="${SKILL_SRC}/SKILL.md"
  if [[ -f "$skill" ]]; then
    # frontmatter description leads with the argus gate. The description may be a
    # folded YAML block, so scan the whole frontmatter (between the first two ---)
    # for the gate signals.
    local fm; fm="$(awk 'NR==1 && /^---/{f=1; next} f && /^---/{exit} f{print}' "$skill")"
    if grep -q 'description:' <<<"$fm" \
       && grep -q '\.argus/worktrees' <<<"$fm" \
       && grep -q 'ARGUS_TASK_ID' <<<"$fm"; then
      ok "skill description names the argus gate"
    else
      bad "skill description does not name the argus gate"
    fi
    # first body section names both gate signals
    if grep -q '\.argus/worktrees' "$skill" && grep -q 'ARGUS_TASK_ID' "$skill"; then
      ok "skill body references both gate signals"
    else
      bad "skill body missing a gate signal"
    fi
  else
    bad "skill file missing: $skill"
  fi

  if [[ -f "$SNIPPET_SRC" ]]; then
    # first content line after frontmatter is the gate
    local body_first; body_first="$(awk 'BEGIN{fm=0} /^---[[:space:]]*$/{fm++; next} fm>=2 && NF{print; exit}' "$SNIPPET_SRC")"
    if grep -qi "ignore this section" <<<"$body_first"; then
      ok "snippet first content line is the gate"
    else
      bad "snippet first content line is not the gate (got: $body_first)"
    fi
    if grep -q '^tags:' "$SNIPPET_SRC" && grep -q '^audience:' "$SNIPPET_SRC"; then
      ok "snippet frontmatter has tags and audience"
    else
      bad "snippet frontmatter missing tags/audience"
    fi
  else
    bad "snippet file missing: $SNIPPET_SRC"
  fi
}

echo "install_test.sh"
echo "skill symlink:"
section_symlink
section_no_clobber
echo "snippet wiring:"
section_snippet_accept
section_snippet_decline
echo "content gates:"
section_content

echo
echo "passed: $PASS  failed: $FAIL"
[[ "$FAIL" -eq 0 ]]

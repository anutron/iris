#!/usr/bin/env bash
# install-claude-skills.sh — install iris's agent-facing assets into ~/.claude.
#
# Prompts (Y/n) separately for:
#   1. symlinking the iris skill into ~/.claude/skills/iris
#   2. appending the orientation snippet into ~/.claude/CLAUDE.md
#
# Idempotent — safe to re-run. Pass --yes to accept both non-interactively.
# Undo with ./uninstall-claude-skills.sh.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=claude/lib-claude-assets.sh
source "${SCRIPT_DIR}/claude/lib-claude-assets.sh"

ASSUME_YES=false
for arg in "$@"; do
  case "$arg" in
    --yes|-y) ASSUME_YES=true ;;
    -h|--help) echo "usage: $0 [--yes]"; exit 0 ;;
    *) echo "unknown flag: $arg" >&2; exit 1 ;;
  esac
done

bold "Install iris Claude skills"
echo

bold "Agent skill"
if confirm "  Symlink the iris skill into ${SKILL_DEST}?"; then
  link_skill
else
  warn "  skipped skill symlink"
fi
echo

bold "Orientation snippet"
if confirm "  Append the iris orientation snippet to ${CLAUDE_MD}?"; then
  append_snippet
else
  warn "  skipped snippet. Wire it in yourself from:"
  echo "    ${SNIPPET_SRC}"
fi
echo

bold "Done."

#!/usr/bin/env bash
# uninstall-claude-skills.sh — remove iris's agent-facing assets from ~/.claude.
#
# Prompts (Y/n) separately for:
#   1. removing the iris skill symlink at ~/.claude/skills/iris
#   2. removing the iris block from ~/.claude/CLAUDE.md
#
# The skill symlink is only removed if it points at THIS repo — a foreign
# symlink or a real directory is left untouched. Idempotent — safe to re-run.
# Pass --yes to accept both non-interactively.

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

bold "Uninstall iris Claude skills"
echo

bold "Agent skill"
if confirm "  Remove the iris skill symlink at ${SKILL_DEST}?"; then
  unlink_skill
else
  warn "  skipped skill symlink removal"
fi
echo

bold "Orientation snippet"
if confirm "  Remove the iris block from ${CLAUDE_MD}?"; then
  remove_snippet_block
else
  warn "  skipped CLAUDE.md block removal"
fi
echo

bold "Done."

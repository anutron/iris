#!/usr/bin/env bash
# lib-claude-assets.sh — shared helpers for install-claude-skills.sh and
# uninstall-claude-skills.sh. SOURCE this file; do not execute it.
#
# Asset paths are computed relative to this file's own location, so the
# install/uninstall scripts work regardless of the cwd they're invoked from.
#
# confirm() honors ASSUME_YES=true for non-interactive runs (the calling
# script sets it from a --yes flag).

LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"   # the claude/ dir
SKILL_SRC="${LIB_DIR}/skills/iris"
SNIPPET_SRC="${LIB_DIR}/snippets/iris.md"
SKILL_DEST="${HOME}/.claude/skills/iris"
CLAUDE_MD="${HOME}/.claude/CLAUDE.md"
SNIPPET_BEGIN="# BEGIN IRIS (argus)"
SNIPPET_END="# END IRIS (argus)"

bold()  { printf '\033[1m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
red()   { printf '\033[31m%s\033[0m\n' "$*" >&2; }
warn()  { printf '\033[33m%s\033[0m\n' "$*"; }

confirm() {
  if [[ "${ASSUME_YES:-false}" == true ]]; then
    return 0
  fi
  local reply
  read -r -p "$1 [Y/n] " reply
  [[ -z "$reply" || "$reply" =~ ^[Yy] ]]
}

# Emit a file's body with a leading "--- ... ---" YAML frontmatter block removed.
strip_frontmatter() {
  awk '
    BEGIN { infm = 0 }
    NR == 1 && $0 ~ /^---[[:space:]]*$/ { infm = 1; next }
    infm == 1 && $0 ~ /^---[[:space:]]*$/ { infm = 0; next }
    infm == 1 { next }
    { print }
  ' "$1"
}

# Symlink the skill into ~/.claude/skills/iris. Idempotent: an existing correct
# symlink is reported and left alone; any other pre-existing path is warned
# about and NOT clobbered.
link_skill() {
  mkdir -p "${HOME}/.claude/skills"
  if [[ -L "${SKILL_DEST}" ]]; then
    local cur; cur="$(readlink "${SKILL_DEST}")"
    if [[ "${cur}" == "${SKILL_SRC}" ]]; then
      green "  ✓ skill already current (${SKILL_DEST})"
      return 0
    fi
    warn "  ${SKILL_DEST} is a symlink to ${cur}; leaving it untouched"
    warn "  (remove it and re-run to relink to ${SKILL_SRC})"
    return 0
  fi
  if [[ -e "${SKILL_DEST}" ]]; then
    warn "  ${SKILL_DEST} exists and is not an iris symlink; leaving it untouched"
    return 0
  fi
  ln -s "${SKILL_SRC}" "${SKILL_DEST}"
  green "  ✓ linked ${SKILL_DEST} → ${SKILL_SRC}"
}

# Remove the skill symlink, but only if it points at THIS repo. Anything else
# (a different symlink, a real directory, nothing) is left alone.
unlink_skill() {
  if [[ -L "${SKILL_DEST}" ]]; then
    local cur; cur="$(readlink "${SKILL_DEST}")"
    if [[ "${cur}" == "${SKILL_SRC}" ]]; then
      rm "${SKILL_DEST}"
      green "  ✓ removed skill symlink ${SKILL_DEST}"
      return 0
    fi
    warn "  ${SKILL_DEST} points to ${cur} (not this repo); leaving it untouched"
    return 0
  fi
  if [[ -e "${SKILL_DEST}" ]]; then
    warn "  ${SKILL_DEST} is not an iris symlink; leaving it untouched"
    return 0
  fi
  green "  ✓ no iris skill symlink to remove"
}

# Append the snippet body (frontmatter stripped) into ~/.claude/CLAUDE.md
# between the marker lines. Re-run replaces the marked block in place.
append_snippet() {
  mkdir -p "$(dirname "${CLAUDE_MD}")"
  touch "${CLAUDE_MD}"

  local body; body="$(strip_frontmatter "${SNIPPET_SRC}")"
  local block; block="${SNIPPET_BEGIN}"$'\n'"${body}"$'\n'"${SNIPPET_END}"

  if grep -qF "${SNIPPET_BEGIN}" "${CLAUDE_MD}"; then
    local existing
    existing="$(awk -v b="${SNIPPET_BEGIN}" -v e="${SNIPPET_END}" '
      f && $0 == e { f = 0 } f { print } $0 == b { f = 1 }' "${CLAUDE_MD}")"
    if [[ "${existing}" == "${body}" ]]; then
      green "  ✓ iris block already up to date in ${CLAUDE_MD}"
      return 0
    fi
    local tmp; tmp="$(mktemp)"
    awk -v b="${SNIPPET_BEGIN}" -v e="${SNIPPET_END}" '
      $0 == b { skip = 1 }
      skip == 0 { print }
      $0 == e { skip = 0 }
    ' "${CLAUDE_MD}" > "${tmp}"
    { cat "${tmp}"; printf '\n%s\n' "${block}"; } > "${CLAUDE_MD}"
    rm -f "${tmp}"
    green "  ✓ updated iris block in ${CLAUDE_MD}"
  else
    printf '\n%s\n' "${block}" >> "${CLAUDE_MD}"
    green "  ✓ appended iris block to ${CLAUDE_MD}"
  fi
}

# Remove the iris marked block from ~/.claude/CLAUDE.md, if present.
remove_snippet_block() {
  if [[ ! -f "${CLAUDE_MD}" ]] || ! grep -qF "${SNIPPET_BEGIN}" "${CLAUDE_MD}"; then
    green "  ✓ no iris block in ${CLAUDE_MD}"
    return 0
  fi
  local tmp; tmp="$(mktemp)"
  awk -v b="${SNIPPET_BEGIN}" -v e="${SNIPPET_END}" '
    $0 == b { skip = 1 }
    skip == 0 { print }
    $0 == e { skip = 0 }
  ' "${CLAUDE_MD}" > "${tmp}"
  cat "${tmp}" > "${CLAUDE_MD}"
  rm -f "${tmp}"
  green "  ✓ removed iris block from ${CLAUDE_MD}"
}

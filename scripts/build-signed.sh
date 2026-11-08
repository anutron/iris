#!/bin/sh
# Build iris and sign it with a STABLE code-signing identity, then install it
# ATOMICALLY at bin/iris. Driven by `make build` (what .iris.toml runs).
#
# Why: iris reads a Keychain item at startup (secrets.op bootstrap_source =
# keychain://...). Keychain "Always Allow" grants are bound to the binary's code
# signature. `go build` output is ad-hoc signed, so its cdhash changes on every
# build and every self-reload re-prompts Touch ID / Keychain. Signing with the
# same self-signed identity and the same --identifier each time keeps the
# designated requirement stable, so one "Always Allow" lasts across rebuilds.
# (Ported from argus scripts/install-signed.sh; same "Argus Code Signing" cert.)
#
# Guarantees:
#   * bin/iris is never half-deployed: we build + sign a temp file next to it
#     and only `mv` it into place after signing succeeded. Any failure leaves
#     the previous binary untouched and exits non-zero with a clear message.
#   * Never hangs on a locked login keychain / pending GUI prompt: a
#     non-interactive preflight fails fast and codesign runs under a hard
#     timeout (alarm armed inside the child, no separate watchdog process).
#
# Env:
#   IRIS_SIGN_IDENTITY     identity name to look up (default "Argus Code Signing")
#   IRIS_SIGN_IDENTIFIER   code-signing identifier (default com.anutron.iris)
#   IRIS_SIGN_TIMEOUT      seconds for codesign/security probes (default 30)
#   IRIS_CODESIGN_TIMEOUT  seconds for codesign alone (default IRIS_SIGN_TIMEOUT)
#   IRIS_SKIP_SIGN=1       deliberately skip signing (escape hatch; also fine on CI)
#   IRIS_BUILD_OUT         destination path (default bin/iris)
#   IRIS_BUILD_CMD         test seam: command run as `$IRIS_BUILD_CMD <out-path>`
set -u

IDENT_NAME="${IRIS_SIGN_IDENTITY:-Argus Code Signing}"
IDENTIFIER="${IRIS_SIGN_IDENTIFIER:-com.anutron.iris}"
TIMEOUT="${IRIS_SIGN_TIMEOUT:-30}"
CS_TIMEOUT="${IRIS_CODESIGN_TIMEOUT:-$TIMEOUT}"
TAG="[build-signed]"

die() { echo "$TAG ERROR: $*" >&2; exit 1; }

# run_timeout SECS CMD... — run CMD, SIGALRM-kill it after SECS; returns 124 on
# timeout. macOS ships no `timeout`. perl `alarm` survives `exec`, so the alarm
# lives in the command itself (a background sleep-watchdog can outlive it and
# misreport an instant success as a timeout — see argus PR #1180).
run_timeout() {
	secs=$1; shift
	perl -e 'alarm shift; exec @ARGV or exit 127' "$secs" "$@" &
	cpid=$!
	wait "$cpid"
	rc=$?
	cpid=
	[ "$rc" -eq 142 ] && return 124 # 128 + SIGALRM
	return $rc
}

dest="${IRIS_BUILD_OUT:-bin/iris}"
mkdir -p "$(dirname "$dest")" || die "cannot create $(dirname "$dest")"

tmp="$dest.new.$$"
# Remove the temp (and codesign sidecars) on EVERY exit path. INT/TERM must
# `exit`, or the script would resume after the signal.
cleanup() {
	[ -n "${cpid:-}" ] && kill "$cpid" 2>/dev/null
	rm -f "$tmp" "$tmp".*
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# 1. Build to a temp path (same dir => same filesystem => atomic rename).
if [ -n "${IRIS_BUILD_CMD:-}" ]; then
	$IRIS_BUILD_CMD "$tmp" || die "build failed"
else
	${GO:-go} build -o "$tmp" ./cmd/iris || die "go build failed"
fi

install_tmp() {
	mv -f "$tmp" "$dest" || die "could not move new binary into place at $dest"
}

# 2. Decide whether to sign.
if [ "${IRIS_SKIP_SIGN:-}" = "1" ]; then
	install_tmp
	echo "$TAG IRIS_SKIP_SIGN=1 — built $dest UNSIGNED (ad-hoc; Keychain will re-prompt after each rebuild)"
	exit 0
fi
# No codesign (Linux CI, containers): nothing to sign with, not an error.
if ! command -v codesign >/dev/null 2>&1; then
	install_tmp
	echo "$TAG codesign not found — built $dest without signing"
	exit 0
fi

command -v perl >/dev/null 2>&1 || die "perl is required to bound codesign/security with a timeout and was not found on PATH (IRIS_SKIP_SIGN=1 builds unsigned without it)"

out=$(run_timeout "$TIMEOUT" security find-identity -p codesigning 2>/dev/null)
id=$(printf '%s\n' "$out" | grep -F "\"$IDENT_NAME\"" | head -1 | awk '{print $2}')
if [ -z "$id" ]; then
	install_tmp
	echo "$TAG WARNING: no '$IDENT_NAME' code-signing identity found — built $dest ad-hoc; Keychain will re-prompt after each rebuild. Install the identity or set IRIS_SKIP_SIGN=1 to silence this."
	exit 0
fi

# 3. Preflight: a locked keychain makes codesign block on an unanswerable GUI prompt.
run_timeout "$TIMEOUT" security show-keychain-info >/dev/null 2>&1
rc=$?
if [ "$rc" -ne 0 ]; then
	die "login keychain is locked or unavailable (security show-keychain-info exit $rc), so signing with '$IDENT_NAME' would hang. Unlock it (security unlock-keychain ~/Library/Keychains/login.keychain-db, or unlock the Mac) and re-run; the existing $dest was NOT modified. To deliberately skip signing: IRIS_SKIP_SIGN=1 make build"
fi

# 4. Sign the TEMP file under a hard timeout.
run_timeout "$CS_TIMEOUT" codesign --force --identifier "$IDENTIFIER" --sign "$id" "$tmp"
rc=$?
if [ "$rc" -eq 124 ]; then
	die "codesign timed out after ${CS_TIMEOUT}s waiting on Keychain access for '$IDENT_NAME' — unlock the Mac / click \"Always Allow\" on the Keychain prompt and re-run; the existing $dest was NOT modified"
elif [ "$rc" -ne 0 ]; then
	die "codesign failed (exit $rc) for '$IDENT_NAME'; the existing $dest was NOT modified"
fi

install_tmp
echo "$TAG signed $dest as $IDENTIFIER with stable identity '$IDENT_NAME' ($id) — Keychain grant persists across rebuilds"

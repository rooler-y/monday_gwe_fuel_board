#!/bin/sh
# Runs the collect+publish cycle in order: refresh from the target DB and
# Samsara, then publish the current snapshot to the Monday Fuel Board.
# Meant to run every 15 minutes, from either host cron or a container
# scheduler (Railway cron, etc.) — path-portable (derives its own directory)
# and POSIX sh, no bashisms.
#
# Best-effort, not fail-fast: each step's failure is logged but does not
# block the next step, since every collector's upserts are independent and
# publish-monday should still push whatever DID update this cycle even if
# one earlier step failed.
#
# Logs to stdout/stderr only (12-factor style) — the caller decides where
# that goes: the host crontab redirects it to logs/cron.log, a container
# platform captures it as the container's own log stream.
#
# flock guards against overlapping runs if a cycle takes longer than its
# schedule interval — a second invocation just exits immediately instead of
# racing the first.

set -u

DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
BIN="$DIR/bin/fuelboard"
LOCK="/tmp/fuelboard-frequent-cycle.lock"

exec 200>"$LOCK"
flock -n 200 || exit 0

cd "$DIR" || exit 1

ts() { date -u +%FT%TZ; }

run() {
	echo "$(ts) start: $1"
	if "$BIN" "$1"; then
		echo "$(ts) ok: $1"
	else
		echo "$(ts) FAILED: $1 (exit $?)" >&2
	fi
}

run collect-db
run collect-samsara-match
run collect-samsara-stats
run publish-monday
run collect-secondary-board
run publish-secondary-board

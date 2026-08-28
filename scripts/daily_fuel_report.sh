#!/bin/sh
# Runs once a day: pulls Samsara's settled 3-day-old MPG report. The next
# frequent_cycle.sh run (within 15 min) publishes whatever this updates, so
# this script doesn't call publish-monday itself. Path-portable and POSIX
# sh — see frequent_cycle.sh for why.

set -u

DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
BIN="$DIR/bin/fuelboard"
LOCK="/tmp/fuelboard-daily-fuel-report.lock"

exec 200>"$LOCK"
flock -n 200 || exit 0

cd "$DIR" || exit 1

ts() { date -u +%FT%TZ; }

echo "$(ts) start: collect-samsara-fuel-report"
if "$BIN" collect-samsara-fuel-report; then
	echo "$(ts) ok: collect-samsara-fuel-report"
else
	echo "$(ts) FAILED: collect-samsara-fuel-report (exit $?)" >&2
fi

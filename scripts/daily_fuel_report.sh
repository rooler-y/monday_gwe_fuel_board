#!/usr/bin/env bash
# Runs once a day via cron: pulls Samsara's settled 3-day-old MPG report.
# The next frequent_cycle.sh run (within 15 min) publishes whatever this
# updates, so this script doesn't call publish-monday itself.

set -u

DIR="/home/yoqubboy_y/Desktop/monday_gwe_fuel_board"
BIN="$DIR/bin/fuelboard"
LOG="$DIR/logs/fuelboard.log"
LOCK="$DIR/logs/daily_fuel_report.lock"

exec 200>"$LOCK"
flock -n 200 || exit 0

cd "$DIR" || exit 1

log() {
	printf '%s %s\n' "$(date -u +%FT%TZ)" "$1" >>"$LOG"
}

log "start: collect-samsara-fuel-report"
if "$BIN" collect-samsara-fuel-report >>"$LOG" 2>&1; then
	log "ok: collect-samsara-fuel-report"
else
	log "FAILED: collect-samsara-fuel-report (exit $?)"
fi

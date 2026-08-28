#!/usr/bin/env bash
# Runs every 15 minutes via cron: refresh from the target DB and Samsara,
# then publish the current snapshot to the Monday Fuel Board.
#
# Best-effort, not fail-fast: each step's failure is logged but does not
# block the next step, since every collector's upserts are independent and
# publish-monday should still push whatever DID update this cycle even if
# one earlier step failed.
#
# flock guards against overlapping runs if a cycle takes longer than 15
# minutes (API slowness etc.) — a second invocation just exits immediately
# instead of racing the first.

set -u

DIR="/home/yoqubboy_y/Desktop/monday_gwe_fuel_board"
BIN="$DIR/bin/fuelboard"
LOG="$DIR/logs/fuelboard.log"
LOCK="$DIR/logs/frequent_cycle.lock"

exec 200>"$LOCK"
flock -n 200 || exit 0

cd "$DIR" || exit 1

log() {
	printf '%s %s\n' "$(date -u +%FT%TZ)" "$1" >>"$LOG"
}

run() {
	log "start: $1"
	if "$BIN" "$1" >>"$LOG" 2>&1; then
		log "ok: $1"
	else
		log "FAILED: $1 (exit $?)"
	fi
}

run collect-db
run collect-samsara-match
run collect-samsara-stats
run publish-monday

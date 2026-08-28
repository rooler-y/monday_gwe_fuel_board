#!/bin/sh
# Docker ENTRYPOINT: applies pending migrations, then execs whatever CMD was
# actually requested (the frequent cycle, the daily fuel report, a one-off
# `migrate`, etc). Runs on every container start — migrate is idempotent
# (tracked via schema_migrations), so this is a fast no-op after the first
# run unless a new migration file was added.
#
# set -e: unlike the cron scripts (best-effort, one step failing doesn't
# block the rest), a failed migration here must hard-stop the container —
# running the real command against a broken/partial schema is worse than
# not starting at all.
set -eu

echo "$(date -u +%FT%TZ) entrypoint: running migrations"
/app/bin/fuelboard migrate

exec "$@"

#!/usr/bin/env bash
#
# Restore a backup taken by scripts/backup.sh, following docs/recovery.md §3.2.
#
# The step this enforces that a hurried operator skips: **stop the engine
# first.** A running engine against a half-restored database claims jobs, runs
# them, and writes results the restore then overwrites — turning a clean
# recovery into a reconciliation problem. This script cannot stop your engine
# for you, so it refuses to start until you confirm it is stopped.
#
# Usage:
#   scripts/restore.sh <backup-directory>
set -euo pipefail

SOURCE="${1:-}"
die() { printf 'restore: %s\n' "$1" >&2; exit 1; }
note() { printf 'restore: %s\n' "$1"; }

[ -n "$SOURCE" ] || die "usage: scripts/restore.sh <backup-directory>"
[ -d "$SOURCE" ] || die "no such backup directory: $SOURCE"
[ -f "${SOURCE}/manifest.txt" ] || die "$SOURCE has no manifest.txt; it was not written by scripts/backup.sh"

note "restoring from:"
sed 's/^/  /' "${SOURCE}/manifest.txt"

if [ "${GOBPM_RESTORE_ASSUME_STOPPED:-}" != "true" ]; then
  printf '\nrestore: is every engine replica stopped? A running engine will overwrite this restore. [type "stopped" to continue] '
  read -r answer
  [ "$answer" = "stopped" ] || die "aborted. Stop the engine, then run again."
fi

# shellcheck disable=SC1091
engine="$(sed -n 's/^engine=//p' "${SOURCE}/manifest.txt")"

case "$engine" in
  postgres)
    command -v pg_restore >/dev/null || die "pg_restore not found"
    [ -f "${SOURCE}/database.dump" ] || die "no database.dump in $SOURCE"
    note "restoring PostgreSQL"
    # --clean --if-exists so a retry onto a partially restored database works;
    # a restore you cannot run twice is one you cannot run under pressure.
    pg_restore --clean --if-exists --no-owner \
      ${DATABASE_URL:+--dbname="$DATABASE_URL"} "${SOURCE}/database.dump"
    ;;
  mysql)
    command -v mysql >/dev/null || die "mysql client not found"
    [ -f "${SOURCE}/database.sql" ] || die "no database.sql in $SOURCE"
    note "restoring MySQL"
    mysql "${MYSQL_DATABASE:?MYSQL_DATABASE is required for a MySQL restore}" < "${SOURCE}/database.sql"
    ;;
  *)
    die "manifest names an unsupported engine: '$engine'"
    ;;
esac

if [ -f "${SOURCE}/encryption-key.gpg" ]; then
  note "the encryption key is in ${SOURCE}/encryption-key.gpg — decrypt it and set ENCRYPTION_KEY before starting:"
  note "  export ENCRYPTION_KEY=\$(gpg --decrypt ${SOURCE}/encryption-key.gpg)"
else
  note "WARNING: this backup carries no encryption key. Restore it from wherever it is kept;"
  note "         without the ORIGINAL key every process and task variable stays unreadable."
fi

cat <<'NEXT'

restore: database restored. What remains, from docs/recovery.md §3.2:

  1. Put ENCRYPTION_KEY and config.yaml in place.
  2. Start the engine. Watch for "Database migrations complete" — a restored
     database may be at an older schema version and the runner brings it forward.
  3. Check /readyz returns 200, then confirm the engine is *working*, not merely
     up: docs/recovery.md §4.

  Expect, and warn people about:
    - timers that should have fired during the lost window firing at once;
    - outbound calls after the recovery point being made again (idempotency keys
      make this safe only for partners that honour them);
    - human tasks completed in the lost window coming back, to be done twice.
NEXT

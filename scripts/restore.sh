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

# explain_failure decides whether a non-zero pg_restore is recoverable.
#
# pg_restore continues past individual errors and then exits non-zero, so the
# same status covers "one statement this server does not understand" and "half
# the tables are missing". Aborting on both is correct by default and useless in
# the case that actually happens: a dump taken with a newer pg_dump emits
# settings an older server rejects — SET transaction_timeout, for instance —
# and a recovery must not be stopped by that with no way forward.
#
# So the errors are printed, and there is a documented way past them. What there
# is not is a silent one.
explain_failure() {
  log="$1"
  status="$2"
  ignored="$(sed -n 's/.*errors ignored on restore: \([0-9]*\).*/\1/p' "$log" | tail -1)"

  if [ -z "$ignored" ] || [ "$ignored" = "0" ]; then
    die "pg_restore exited ${status} without completing; see ${log}"
  fi

  note ""
  note "pg_restore ignored ${ignored} error(s) and exited ${status}:"
  grep -iE '^pg_restore: (error|warning)' "$log" | head -20 | sed 's/^/  /'
  note ""
  note "Read them. A version skew and a table that failed to restore look the same"
  note "from here, and only one of them is safe to start an engine on."

  if [ "${METIS_RESTORE_IGNORE_ERRORS:-}" = "true" ]; then
    note "Continuing because METIS_RESTORE_IGNORE_ERRORS=true."
    return 0
  fi
  die "stopping. Once you have read the errors above and decided they are safe, re-run with METIS_RESTORE_IGNORE_ERRORS=true"
}

note "restoring from:"
sed 's/^/  /' "${SOURCE}/manifest.txt"

if [ "${METIS_RESTORE_ASSUME_STOPPED:-}" != "true" ]; then
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
    #
    # The output is captured because pg_restore exits 0 having ignored errors.
    # That is its documented default, and it means a restore that only partly
    # worked reports success — which a rehearsal caught here: a version-skewed
    # dump logged "errors ignored on restore: 1" and this script said nothing.
    # A benign skew and a missing table look identical at that point, so the
    # operator has to be the one who decides.
    set +e
    pg_restore --clean --if-exists --no-owner \
      ${DATABASE_URL:+--dbname="$DATABASE_URL"} "${SOURCE}/database.dump" 2>&1 | tee "${SOURCE}/restore.log"
    restore_status=${PIPESTATUS[0]}
    set -e
    if [ "$restore_status" -ne 0 ]; then
      # Order matters: the ignored-error case has to be examined before the
      # exit status is treated as fatal, or the override below can never be
      # reached and a benign version skew stops a recovery dead.
      explain_failure "${SOURCE}/restore.log" "$restore_status"
    fi
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

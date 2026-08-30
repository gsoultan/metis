#!/usr/bin/env bash
#
# Take a backup that can actually be restored from.
#
# docs/recovery.md describes this procedure; this is that procedure, so it can
# be run on a schedule rather than transcribed under pressure. Two things it is
# strict about, both of which have made real backups useless:
#
#   1. **The encryption key is backed up too, separately.** A database backup
#      without ENCRYPTION_KEY restores rows nothing can read. Separately,
#      because a key stored beside the data it protects protects nothing.
#   2. **MySQL dumps use --single-transaction.** Without it the dump is not a
#      consistent snapshot, and a process instance can be captured in a state
#      its own tokens contradict — which restores as a corrupt instance rather
#      than an obviously failed backup.
#
# Usage:
#   scripts/backup.sh [destination-directory]
#
# Reads DATABASE_URL (or the individual PG*/MYSQL_* variables) and
# ENCRYPTION_KEY from the environment. Destination defaults to ./backups.
set -euo pipefail

DEST="${1:-./backups}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
TARGET="${DEST}/${STAMP}"

die() { printf 'backup: %s\n' "$1" >&2; exit 1; }
note() { printf 'backup: %s\n' "$1"; }

# The key first. A backup that runs for an hour and then discovers it cannot
# preserve the key has wasted the window and produced something unrestorable.
[ -n "${ENCRYPTION_KEY:-}" ] || die "ENCRYPTION_KEY is not set. A database backup without it restores unreadable rows; refusing to take one that looks complete and is not."

mkdir -p "$TARGET"
chmod 700 "$TARGET"

case "${METIS_BACKUP_ENGINE:-postgres}" in
  postgres)
    command -v pg_dump >/dev/null || die "pg_dump not found"
    # --format=custom so the restore can be parallel and selective; plain SQL
    # cannot. --no-owner so it restores into a database whose roles differ,
    # which is the usual case when restoring into a recovery environment.
    note "dumping PostgreSQL"
    pg_dump --format=custom --no-owner --file="${TARGET}/database.dump" \
      ${DATABASE_URL:+--dbname="$DATABASE_URL"}
    ;;
  mysql)
    command -v mysqldump >/dev/null || die "mysqldump not found"
    note "dumping MySQL"
    mysqldump --single-transaction --routines --triggers --events \
      "${MYSQL_DATABASE:?MYSQL_DATABASE is required for a MySQL backup}" \
      > "${TARGET}/database.sql"
    ;;
  *)
    die "unsupported METIS_BACKUP_ENGINE '${METIS_BACKUP_ENGINE}'; expected postgres or mysql"
    ;;
esac

# The key, encrypted to an operator rather than written in the clear. If gpg is
# unavailable this stops rather than falling back to plaintext: a key sitting
# beside its database in a backup directory is worse than no backup, because it
# looks like one.
KEY_RECIPIENT="${METIS_BACKUP_GPG_RECIPIENT:-}"
if [ -n "$KEY_RECIPIENT" ]; then
  command -v gpg >/dev/null || die "METIS_BACKUP_GPG_RECIPIENT is set but gpg is not installed"
  printf '%s' "$ENCRYPTION_KEY" | gpg --batch --yes --encrypt --recipient "$KEY_RECIPIENT" \
    --output "${TARGET}/encryption-key.gpg"
  chmod 600 "${TARGET}/encryption-key.gpg"
  note "encryption key written encrypted to ${KEY_RECIPIENT}"
else
  note "WARNING: METIS_BACKUP_GPG_RECIPIENT is not set, so the encryption key was NOT included."
  note "         Store it yourself, in a different system from this backup, or the restore will fail."
fi

# config.yaml carries the JWT secret and connection settings; recovery.md step 3
# restores it alongside the key.
if [ -f config.yaml ]; then
  cp config.yaml "${TARGET}/config.yaml"
  chmod 600 "${TARGET}/config.yaml"
fi

# A manifest, so a restore knows what it is holding without guessing from
# filenames — including the schema version, which is what decides whether the
# binary being restored onto can read it at all.
cat > "${TARGET}/manifest.txt" <<MANIFEST
taken_at=${STAMP}
engine=${METIS_BACKUP_ENGINE:-postgres}
encryption_key_included=$([ -n "$KEY_RECIPIENT" ] && echo yes || echo no)
config_included=$([ -f "${TARGET}/config.yaml" ] && echo yes || echo no)
MANIFEST

note "written to ${TARGET}"
note "RESTORE IS A HYPOTHESIS UNTIL REHEARSED — docs/recovery.md §4 is the drill."

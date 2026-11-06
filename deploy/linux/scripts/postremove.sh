#!/bin/sh
# Forgets the removed unit. Deliberately keeps /var/lib/metis, /etc/metis and
# the metis user, even on purge: /var/lib/metis holds config.yaml, which carries
# the encryption key the database was sealed with, and deleting it would make
# every encrypted value in that database unreadable. Remove them by hand once
# the database is gone too.
set -e

if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
	systemctl daemon-reload >/dev/null 2>&1 || true
fi

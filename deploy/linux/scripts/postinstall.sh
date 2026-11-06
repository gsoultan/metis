#!/bin/sh
# Reload systemd so it sees the unit. Deliberately does not enable, start or
# restart the service:
#
# - On a fresh install there is no DATABASE_URL yet, so a start would only
#   come up in pre-setup mode on an address nobody chose.
# - On an upgrade the new binary may carry schema migrations, and
#   docs/upgrading.md asks for a backup before the first start of a new
#   release. The running server keeps running the old binary until you restart
#   it yourself.
set -e

if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

cat <<'MSG'
metis is installed. To run it:
  1. set DATABASE_URL (and the secrets) in /etc/metis/metis.env
  2. systemctl enable --now metis
After an upgrade, take a backup (docs/upgrading.md), then: systemctl restart metis
MSG

exit 0

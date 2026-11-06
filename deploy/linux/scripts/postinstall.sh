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

# The package declares these owners, but pacman unpacks by numeric id, and the
# metis account's id is only known on this host, so on Arch they come out
# root:root. Set them here so every format ends the same way. Not recursive:
# whatever the service wrote under /var/lib/metis is already its own.
chown root:metis /etc/metis /etc/metis/metis.env
chmod 0750 /etc/metis
chmod 0640 /etc/metis/metis.env
chown metis:metis /var/lib/metis
chmod 0750 /var/lib/metis

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

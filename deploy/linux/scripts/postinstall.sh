#!/bin/sh
# Reload systemd so it sees the unit. Deliberately does not enable, start or
# restart the service:
#
# - On a fresh install starting it is the operator's call: it serves the
#   setup wizard to anyone who can reach the port (behind the setup token in
#   the log), and they may want a firewall or an address chosen first.
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
    # The Upgrades page's button: it watches for a request file and starts
    # nothing on its own. Disable it to keep upgrades to the command line.
    # Enabled on the first install only, so an operator who disabled it is
    # not overruled by the next upgrade.
    if [ ! -e /var/lib/metis/.upgrade-path-offered ]; then
        systemctl enable --now metis-upgrade.path >/dev/null 2>&1 || true
        : > /var/lib/metis/.upgrade-path-offered
    fi
fi

cat <<'MSG'
metis is installed. To run it:
  1. systemctl enable --now metis
  2. open http://<this server>:8080/setup and choose the database there;
     the setup token is in: journalctl -u metis | grep "setup token"
  (or set DATABASE_URL and the secrets in /etc/metis/metis.env first)
After an upgrade, take a backup (docs/upgrading.md), then: systemctl restart metis
MSG

exit 0

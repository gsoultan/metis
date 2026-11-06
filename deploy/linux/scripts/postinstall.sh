#!/bin/sh
# Tells the init system about the service and says what is left to do. It does
# not enable, start or restart the service: Metis cannot start until
# /etc/metis/metis.env names a database and its secrets, and an upgrade runs the
# new release's migrations as it starts, which is the operator's moment to
# choose (docs/upgrading.md: take a backup first).
set -e

# The package declares these owners, but not every package manager restores
# them: pacman unpacks by numeric id, and the metis user's id is only known on
# this host. Set them here so every format ends the same way. Not recursive:
# what the service wrote under /var/lib/metis is already its own.
chown root:metis /etc/metis /etc/metis/metis.env
chmod 0750 /etc/metis
chmod 0640 /etc/metis/metis.env
chown metis:metis /var/lib/metis
chmod 0750 /var/lib/metis

if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
	systemctl daemon-reload >/dev/null 2>&1 || true
	if systemctl is-active --quiet metis 2>/dev/null; then
		echo "metis: the running service is still the old version. Restart it when ready: systemctl restart metis"
		exit 0
	fi
	echo "metis: fill in /etc/metis/metis.env, then: systemctl enable --now metis"
elif command -v rc-service >/dev/null 2>&1; then
	echo "metis: fill in /etc/metis/metis.env, then: rc-update add metis default && rc-service metis start"
else
	echo "metis: fill in /etc/metis/metis.env and run /usr/bin/metis as the metis user from /var/lib/metis"
fi

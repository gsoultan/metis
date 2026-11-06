#!/bin/sh
# Stops the service when the package is removed, and leaves it running when the
# package is only being upgraded.
#
# How an upgrade is told apart differs by packager: dpkg passes `upgrade`, rpm
# passes the number of versions that will remain (1 or more on an upgrade, 0 on
# removal). apk and pacman run this script only on removal.
set -e

case "${1:-}" in
	upgrade | failed-upgrade) exit 0 ;;
	'' | *[!0-9]*) ;;
	*) [ "$1" -ge 1 ] && exit 0 ;;
esac

if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
	systemctl disable --now metis >/dev/null 2>&1 || true
elif command -v rc-service >/dev/null 2>&1; then
	rc-service metis stop >/dev/null 2>&1 || true
	rc-update del metis default >/dev/null 2>&1 || true
fi

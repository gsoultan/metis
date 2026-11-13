#!/bin/sh
# Stop and disable the service when the package is removed, but not when it is
# upgraded: an upgrade leaves the running server alone (see postinstall.sh).
#
# How each format says "removal": deb passes "remove" or "purge", rpm passes 0
# (the number of versions left), and apk and Arch call this script only for a
# removal and pass no argument.
set -e

case "$1" in
    remove|purge|0|"")
        if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
            systemctl disable --now metis-upgrade.path >/dev/null 2>&1 || true
            systemctl disable --now metis.service >/dev/null 2>&1 || true
        fi
        ;;
esac

exit 0

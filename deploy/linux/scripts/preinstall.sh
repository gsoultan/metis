#!/bin/sh
# Creates the metis system user before the package's files are unpacked, so the
# directories the package owns can be owned by it. Runs for deb, rpm, apk and
# Arch alike, so it uses whichever tool the system has.
set -e

if ! getent group metis >/dev/null 2>&1; then
	if command -v groupadd >/dev/null 2>&1; then
		groupadd --system metis
	else
		addgroup -S metis
	fi
fi

if ! getent passwd metis >/dev/null 2>&1; then
	nologin=/usr/sbin/nologin
	[ -x "$nologin" ] || nologin=/sbin/nologin
	if command -v useradd >/dev/null 2>&1; then
		useradd --system --gid metis --home-dir /var/lib/metis --no-create-home \
			--shell "$nologin" --comment "Metis BPM" metis
	else
		adduser -S -D -H -h /var/lib/metis -s "$nologin" -G metis -g "Metis BPM" metis
	fi
fi

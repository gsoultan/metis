#!/bin/sh
# Create the unprivileged system account the service runs as. Idempotent, so
# upgrades and reinstalls go through it unchanged.
set -e

if ! getent group metis >/dev/null 2>&1; then
    if command -v groupadd >/dev/null 2>&1; then
        groupadd --system metis
    else
        addgroup -S metis
    fi
fi

if ! getent passwd metis >/dev/null 2>&1; then
    if command -v useradd >/dev/null 2>&1; then
        useradd --system --gid metis --home-dir /var/lib/metis --no-create-home \
            --shell /usr/sbin/nologin --comment "Metis BPM" metis 2>/dev/null ||
        useradd --system --gid metis --home-dir /var/lib/metis --no-create-home \
            --shell /sbin/nologin --comment "Metis BPM" metis
    else
        adduser -S -D -H -h /var/lib/metis -s /sbin/nologin -G metis -g "Metis BPM" metis
    fi
fi

exit 0

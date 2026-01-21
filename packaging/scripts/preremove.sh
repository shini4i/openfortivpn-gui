#!/bin/sh
set -e

# Stop and disable service if running
if command -v systemctl >/dev/null 2>&1; then
    systemctl stop openfortivpn-gui-helper 2>/dev/null || true
    systemctl disable openfortivpn-gui-helper 2>/dev/null || true
fi

# Note: Do NOT remove the group - users may still be members

exit 0

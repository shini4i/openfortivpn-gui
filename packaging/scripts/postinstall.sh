#!/bin/sh
set -e

# Create system group for socket access (if not exists)
# Handle TOCTOU race: attempt creation, verify on failure
if ! getent group openfortivpn-gui >/dev/null 2>&1; then
    if ! groupadd -r openfortivpn-gui 2>/dev/null; then
        # groupadd failed - check if another process created it
        if ! getent group openfortivpn-gui >/dev/null 2>&1; then
            echo "Error: Failed to create openfortivpn-gui group" >&2
            exit 1
        fi
    fi
fi

# Reload systemd to pick up new service file
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

# Print post-install instructions (no auto-enable for security)
echo ""
echo "openfortivpn-gui installed successfully."
echo ""
echo "To enable passwordless VPN operations:"
echo "  1. Add your user to the group: sudo usermod -aG openfortivpn-gui \$USER"
echo "  2. Log out and back in"
echo "  3. Enable the helper: sudo systemctl enable --now openfortivpn-gui-helper"
echo ""

exit 0

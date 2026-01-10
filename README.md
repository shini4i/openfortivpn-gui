# openfortivpn-gui

[![codecov](https://codecov.io/gh/shini4i/openfortivpn-gui/graph/badge.svg?token=PSGP25XQ4K)](https://codecov.io/gh/shini4i/openfortivpn-gui)

A modern GTK4/libadwaita GUI client for Fortinet SSL VPN on Linux, wrapping the [openfortivpn](https://github.com/adrienverge/openfortivpn) CLI tool.

## Features

- **Multiple VPN Profiles** - Create, edit, and manage multiple VPN connection profiles
- **Multiple Authentication Methods**:
  - Username/Password
  - One-Time Password (OTP)
  - Client Certificate
  - SAML/SSO (browser-based authentication)
- **System Tray Integration** - Minimize to tray, quick connect/disconnect
- **Desktop Notifications** - Connection status notifications
- **Secure Credential Storage** - Passwords stored in system keyring (GNOME Keyring/libsecret)
- **Auto-Connect** - Optionally connect to last used profile on startup
- **Configurable Routing Options**:
  - DNS configuration (set-dns)
  - Route management (set-routes)
  - Half-internet routes (split tunneling with /1 routes)

## Requirements

- Linux with GTK4 and libadwaita
- [openfortivpn](https://github.com/adrienverge/openfortivpn) installed
- polkit (for privilege escalation via pkexec)
- libsecret/GNOME Keyring (for secure credential storage)

## Installation

### Building from Source

**Prerequisites:**
- Go (see `go.mod` for version)
- GTK4 development libraries
- libadwaita development libraries
- pkg-config

**Using Nix (recommended):**
```bash
# Enter development shell with all dependencies
nix develop

# Build
task build

# Run
task run
```

**Manual build:**
```bash
# Install dependencies (Debian/Ubuntu)
sudo apt install golang libgtk-4-dev libadwaita-1-dev pkg-config

# Build
go build -o openfortivpn-gui .

# Run
./openfortivpn-gui
```

## Usage

1. **Launch the application** - Run `openfortivpn-gui`
2. **Create a profile** - Click the "+" button to add a new VPN profile
3. **Configure the profile**:
   - Enter VPN server hostname and port
   - Select authentication method
   - Configure routing options as needed
4. **Connect** - Select a profile and click "Connect"
5. **Authenticate** - Enter password when prompted (or complete SAML flow in browser)

### Environment Variables

| Variable | Description |
|----------|-------------|
| `OPENFORTIVPN_GUI_DEBUG` | Set to `1` to enable debug logging |

## Security

- **Passwords are never stored in plaintext** - All credentials are stored in the system keyring
- **Passwords are passed via stdin** - Never exposed in command-line arguments or process listings
- **Profile validation** - Strict input validation prevents command injection

## Contributing

Contributions are welcome! Please feel free to submit issues and pull requests.

## License

GPL-3.0 - see [LICENSE](LICENSE) for details.

## Acknowledgments

- [openfortivpn](https://github.com/adrienverge/openfortivpn) - The underlying VPN client
- [gotk4](https://github.com/diamondburned/gotk4) - Go bindings for GTK4

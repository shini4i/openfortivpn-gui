<h1 align="center">openfortivpn-gui</h1>

<p align="center">
  <img src="https://img.shields.io/github/go-mod/go-version/shini4i/openfortivpn-gui" alt="GitHub go.mod Go version">
  <img src="https://img.shields.io/github/v/release/shini4i/openfortivpn-gui" alt="GitHub release">
  <a href="https://goreportcard.com/report/github.com/shini4i/openfortivpn-gui"><img src="https://goreportcard.com/badge/github.com/shini4i/openfortivpn-gui" alt="Go Report Card"></a>
  <img src="https://img.shields.io/github/license/shini4i/openfortivpn-gui" alt="GitHub license">
</p>

<p align="center">
  <img src="https://raw.githubusercontent.com/shini4i/assets/main/src/openfortivpn-gui/screenshot.png" alt="openfortivpn-gui screenshot" width="800">
</p>

<p align="center">
  A modern GTK4/libadwaita GUI client for Fortinet SSL VPN on Linux, wrapping the <a href="https://github.com/adrienverge/openfortivpn">openfortivpn</a> CLI tool.
</p>

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

### NixOS (with binary cache for fast installs)

```bash
# One-time setup: enable the binary cache
cachix use shini4i

# Install
nix profile install github:shini4i/nixpkgs#openfortivpn-gui
```

### Debian/Ubuntu

Download the `.deb` package from [GitHub Releases](https://github.com/shini4i/openfortivpn-gui/releases) and install:

```bash
sudo apt install ./openfortivpn-gui_*.deb
```

### Fedora/RHEL

Download the `.rpm` package from [GitHub Releases](https://github.com/shini4i/openfortivpn-gui/releases) and install:

```bash
sudo dnf install ./openfortivpn-gui-*.rpm
```

### Post-Installation Setup (deb/rpm only)

To enable passwordless VPN operations with the helper daemon:

1. Add your user to the group:

   ```bash
   sudo usermod -aG openfortivpn-gui $USER
   ```

2. Log out and back in (for group membership to take effect)

3. Enable the helper service:

   ```bash
   sudo systemctl enable --now openfortivpn-gui-helper
   ```

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
go build -o openfortivpn-gui ./cmd/openfortivpn-gui

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

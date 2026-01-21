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
- **Multiple Authentication Methods**: Username/Password, OTP, Client Certificate, SAML/SSO
- **System Tray Integration** - Minimize to tray, quick connect/disconnect
- **Desktop Notifications** - Connection status notifications
- **Secure Credential Storage** - Passwords stored in system keyring (libsecret)
- **Auto-Connect** - Optionally connect to last used profile on startup
- **Configurable Routing** - DNS, routes, and split tunneling options

## Installation

### NixOS / Nix

Add [shini4i/nixpkgs](https://github.com/shini4i/nixpkgs) as a flake input or install directly:

```bash
# Enable binary cache for faster installs
cachix use shini4i

# Install
nix profile install github:shini4i/nixpkgs#openfortivpn-gui
```

A NixOS module is also available for declarative configuration.

### Fedora

Download the `.rpm` package from [GitHub Releases](https://github.com/shini4i/openfortivpn-gui/releases):

```bash
sudo dnf install ./openfortivpn-gui-*.rpm
```

After installation, enable passwordless VPN operations:

```bash
sudo usermod -aG openfortivpn-gui $USER
# Log out and back in, then:
sudo systemctl enable --now openfortivpn-gui-helper
```

### Debian/Ubuntu

> **Note:** Debian/Ubuntu packages are not available yet due to libadwaita version requirements (needs 1.7+). Packages will be provided once compatible versions reach Debian/Ubuntu repositories.

### Building from Source

```bash
# Enter development shell with all dependencies
nix develop

# Build and run
task build
task run
```

## Usage

1. Launch `openfortivpn-gui`
2. Click "+" to create a VPN profile
3. Configure server, authentication method, and routing options
4. Select a profile and click "Connect"

Set `OPENFORTIVPN_GUI_DEBUG=1` for debug logging.

## License

GPL-3.0 - see [LICENSE](LICENSE) for details.

## Acknowledgments

- [openfortivpn](https://github.com/adrienverge/openfortivpn) - The underlying VPN client
- [gotk4](https://github.com/diamondburned/gotk4) - Go bindings for GTK4

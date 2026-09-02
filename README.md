<h1 align="center">openfortivpn-gui</h1>

<p align="center">
  <img src="https://img.shields.io/github/go-mod/go-version/shini4i/openfortivpn-gui" alt="GitHub go.mod Go version">
  <img src="https://img.shields.io/github/v/release/shini4i/openfortivpn-gui" alt="GitHub release">
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
- **System Tray Integration** - Minimize to tray, quick connect/disconnect ([setup notes](#system-tray))
- **Desktop Notifications** - Connection status notifications
- **Secure Credential Storage** - Passwords stored in system keyring (libsecret); a password the gateway rejects is discarded so the next connection prompts for it again. On 2FA profiles, where a rejected one-time token is indistinguishable from a rejected password, the profile editor's **Forget** button clears the saved password instead. Switching a profile to certificate or SAML authentication discards its saved password too
- **Auto-Connect** - Optionally connect to last used profile on startup
- **Auto Reconnect** - Automatically reconnect if the connection drops unexpectedly (configurable per profile)
- **Configurable Routing** - DNS, routes, and split tunneling (half-internet routes)

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


### Arch
> [!NOTE]
This package is not officially maintained by the author. For questions or issues, please open an issue on the AUR package page, not this repository.

Install package from AUR
```bash
yay -S openfortivpn-gui-bin
```

After installation, enable passwordless VPN operations:

```bash
sudo usermod -aG openfortivpn-gui $USER
# Log out and back in
```

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

> [!WARNING]
Requires libadwaita 1.7+, available on Debian 13 (Trixie) and later, and Ubuntu 25.04 and later. Older releases (e.g. Ubuntu 24.04 LTS) ship an incompatible libadwaita and are not supported.

Download the `.deb` package from [GitHub Releases](https://github.com/shini4i/openfortivpn-gui/releases):

```bash
sudo apt install ./openfortivpn-gui_*.deb
```

After installation, enable passwordless VPN operations:

```bash
sudo usermod -aG openfortivpn-gui $USER
# Log out and back in, then:
sudo systemctl enable --now openfortivpn-gui-helper
```

### Verifying Releases

Releases ship SBOMs and a keyless [cosign](https://docs.sigstore.dev/cosign/installation/) signature over the checksum file:

```bash
cosign verify-blob \
  --bundle openfortivpn-gui_<version>_checksums.txt.sigstore.json \
  --certificate-identity-regexp 'https://github.com/shini4i/openfortivpn-gui' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  openfortivpn-gui_<version>_checksums.txt
```

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

Set `OPENFORTIVPN_GUI_DEBUG=1` for debug logging (the helper daemon also accepts a `-debug` flag).

### System Tray

Tray integration uses the [StatusNotifierItem](https://www.freedesktop.org/wiki/Specifications/StatusNotifierItem/) (SNI) D-Bus protocol — the cross-desktop standard supported by KDE Plasma, XFCE, Waybar, and most panels. On startup the app probes D-Bus for a registered SNI host: if one is found and at least one profile exists, it starts minimized to tray; otherwise the main window is presented so the UI is never unreachable.

Two environments need extra setup for tray mode:

- **GNOME** ships no SNI host by default. Install the [AppIndicator and KStatusNotifierItem Support](https://extensions.gnome.org/extension/615/appindicator-support/) extension.
- **Legacy X11 panels** that only speak the older XEmbed tray spec need a bridge such as [snixembed](https://git.sr.ht/~steef/snixembed).

> [!NOTE]
> There is no in-app workaround for a missing SNI host. XEmbed is X11-only and unavailable under Wayland, and GTK4 removed `GtkStatusIcon` entirely — without a host process listening on the bus, there is no tray to render into.

## License

GPL-3.0 - see [LICENSE](LICENSE) for details.

## Acknowledgments

- [openfortivpn](https://github.com/adrienverge/openfortivpn) - The underlying VPN client
- [gotk4](https://github.com/diamondburned/gotk4) - Go bindings for GTK4

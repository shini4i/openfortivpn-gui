# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.4.0] - 2026-08-21

### Added

- A "Saved Password" row in the profile editor's Authentication section, with a
  **Forget** button that removes the profile's password from the system keyring
  so the next connection prompts for it again. This is the only way to replace a
  stored password on a 2FA profile: openfortivpn reports a rejected one-time
  token with the same error as a rejected password, so those profiles are
  excluded from the automatic discard.

### Changed

- The username, realm and trusted-certificate fields are now checked for
  control characters and a maximum length when connecting, matching the checks
  already applied to the profile name, description and host. The limits are 256
  characters for username and realm and 128 for a trusted certificate, so
  ordinary values are unaffected.

### Fixed

- On hosts with no `resolvconf` binary, the VPN's DNS servers are now applied
  when connecting through the helper daemon. The helper's systemd sandbox
  mounted the whole filesystem read-only, so openfortivpn could not update
  `/etc/resolv.conf`: the tunnel came up but the gateway's name servers were
  silently ignored, leaving DNS queries on the host's original resolvers.
  Hosts that provide `resolvconf` — which includes anything running
  systemd-resolved — were unaffected, as openfortivpn uses that instead.
- Closed a race in the system tray's startup that could abort the application.
  Tray menu entries were created on the tray's own thread while the main window
  could already be updating them, so an update could reach an entry that did
  not exist yet. Whether it did depended on thread timing.
- Certificate-authentication profiles can now connect. Connecting demanded a
  keyring password for every method except SAML, so a certificate profile was
  held behind a prompt for a credential it has no concept of — and entering an
  arbitrary value to get past it was the only way through. Auto-reconnect no
  longer refuses these profiles for the same reason.
- Submitting the password dialog with an empty field no longer dismisses it and
  silently abandons the connection. Connect stays disabled until a password is
  entered, so the dialog can only close by connecting or being cancelled.
- A password the gateway rejects is now discarded from the keyring, so the next
  connection prompts for it again. Previously the password was stored before it
  was ever validated and reused unchecked from then on, leaving a mistyped one
  cached with no way to correct it short of deleting the profile. Profiles using
  a one-time password are excluded: a rejected token reports the same error as a
  rejected password, and acting on it would discard a working password.
- The `openfortivpn_path` setting in `config.json` now takes effect for
  connections made without the helper daemon. It was saved and validated but
  never read, so a binary installed outside the expected location could not be
  selected. If the configured path cannot be found, the binary is looked up on
  `PATH` as before. The helper daemon resolves its own binary — deliberately,
  since accepting a path over its socket would let any member of the
  `openfortivpn-gui` group choose what runs as root — so the setting is
  reported as unused when connecting through it. Point the daemon elsewhere
  with its `-openfortivpn` flag.
- The address assigned by the VPN is no longer left on screen while the
  connection is being re-established. It was only cleared on disconnect and
  failure, so the previous tunnel's address stayed visible under
  "Connecting…" and "Reconnecting…".
- Preference changes are saved as soon as they are toggled. They were only
  written when the preferences window was closed, so quitting from the tray
  with it still open discarded the change.
- The line explaining why a connection failed now reaches the connection log
  and the error dialog. openfortivpn's last output was read from a pipe that
  was closed the moment the process was reaped, so the decisive error could be
  lost and a failure could be reported as a plain disconnect. The error also
  now always arrives before the connection's final state, which is what lets a
  gateway-rejected password be discarded from the keyring.

## [0.3.6] - 2026-07-27

### Fixed

- Editing an existing profile no longer silently reverts on the next
  connection. The profile list kept a stale entry after a save, so re-selecting
  the edited profile served pre-edit data and a subsequent connect could
  overwrite the saved change with the old values. The delete confirmation
  dialog likewise showed the pre-edit name for a just-edited profile and now
  reflects the saved values.
- Entering an invalid one-time password no longer silently dismisses the 2FA
  dialog and abandons the connection. The Submit button stays disabled until a
  valid token is entered, so the dialog can no longer close without either
  submitting a valid OTP or being cancelled.
- In environments without system tray support (for example, GNOME with no
  AppIndicator extension), the main window now opens on startup instead of
  staying hidden behind a tray icon that never renders. Previously the app
  could start with no visible window and no working tray icon, leaving the UI
  unreachable. Where a tray is available, startup is unchanged — the window
  stays hidden when a profile already exists.

## [0.3.5] - 2026-06-28

### Changed

- The system tray icon is now a clean hollow shield outline that changes color
  by connection status — gray when disconnected, orange while connecting, green
  when connected — replacing the previous generic padlock. The minimal outline
  stays sharp and legible at small tray sizes, where the detailed shield artwork
  blurred into an unreadable smudge.

### Fixed

- Enforce a minimum 1-second reconnect delay so that exponential backoff and
  jitter are always applied, even when `reconnect_delay_seconds` is set to zero.
- Fix a potential invalid UTF-8 byte in structured logs when the helper daemon
  sends an unknown protocol message longer than 200 bytes.

## [0.3.4] - 2026-06-16

### Added

- Application icons (XDG hicolor set) are now installed by the deb and rpm
  packages, so the desktop entry shows a proper icon. The icon and desktop
  databases are refreshed on install.
- Release artifacts are now signed with a keyless cosign signature over the
  checksum file, and SBOMs are published alongside the packages and archives.

### Fixed

- Profile settings are now preserved when saving an existing profile.

### Security

- Hardened the privileged helper daemon. Its systemd unit now runs sandboxed
  (`NoNewPrivileges`, namespace/syscall/address-family restrictions,
  `MemoryDenyWriteExecute`, and related protections).

[Unreleased]: https://github.com/shini4i/openfortivpn-gui/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/shini4i/openfortivpn-gui/compare/v0.3.6...v0.4.0
[0.3.6]: https://github.com/shini4i/openfortivpn-gui/compare/v0.3.5...v0.3.6
[0.3.5]: https://github.com/shini4i/openfortivpn-gui/compare/v0.3.4...v0.3.5
[0.3.4]: https://github.com/shini4i/openfortivpn-gui/compare/v0.3.3...v0.3.4

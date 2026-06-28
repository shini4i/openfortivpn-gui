# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/shini4i/openfortivpn-gui/compare/v0.3.4...HEAD
[0.3.4]: https://github.com/shini4i/openfortivpn-gui/compare/v0.3.3...v0.3.4

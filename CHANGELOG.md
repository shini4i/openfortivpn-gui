# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/shini4i/openfortivpn-gui/compare/v0.3.3...HEAD

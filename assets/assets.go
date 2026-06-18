// Package assets embeds application image assets so they are available to the
// compiled binary without relying on files on disk.
package assets

import _ "embed"

// ShieldIconPNG is the application shield icon used as the base artwork for the
// system tray. It is the 48x48 variant: large enough to stay crisp when the
// tray host scales it up on HiDPI panels, small enough to keep the binary lean.
//
//go:embed icons/hicolor/48x48/apps/openfortivpn-gui.png
var ShieldIconPNG []byte

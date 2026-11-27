// Package appicon embeds the app icon the app shows itself (the 关于
// page). The files are generated: see README.md.
package appicon

import _ "embed"

// MacPNG is the macOS icon at 256 px (src/raster.go).
//
//go:embed macos/twinnel-256.png
var MacPNG []byte

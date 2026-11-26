// Package icons embeds duet's icon files (see README.md).
package icons

import "embed"

// FS holds the SVG icons at the root and the tray PNGs under tray/.
//
//go:embed *.svg tray/*.png
var FS embed.FS

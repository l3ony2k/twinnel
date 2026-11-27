// Package icons embeds Twinnel's icon files (see README.md).
package icons

import "embed"

// FS holds the SVG icons at the root and the menu bar PNGs under tray/
// (<state>.png at @2x, <state>@1x.png).
//
//go:embed *.svg tray/*.png
var FS embed.FS

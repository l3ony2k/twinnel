// Package overrides embeds the built-in override templates.
package overrides

import "embed"

// Templates holds the *.yaml templates of this directory.
//
//go:embed *.yaml
var Templates embed.FS

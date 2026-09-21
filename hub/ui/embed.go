// Package ui embeds the built panel. `npm run build` in this directory
// writes dist/; the Go build carries it into the binary.
package ui

import "embed"

//go:embed all:dist
var Dist embed.FS

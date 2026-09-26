// Package web embeds the built single-page app (web/dist) into the binary.
//
// `make web` (pnpm build) fills dist/; a committed dist/.gitkeep keeps the
// embed pattern valid on fresh clones, in which case the server shows a
// placeholder page instead of the UI.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the contents of web/dist.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // the embed directive guarantees dist exists
	}
	return sub
}

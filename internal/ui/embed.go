package ui

import (
	"embed"
	"io/fs"
)

//go:embed static
var embeddedFiles embed.FS

// Assets returns the embedded admin UI files, rooted at the static directory.
func Assets() (fs.FS, error) {
	return fs.Sub(embeddedFiles, "static")
}

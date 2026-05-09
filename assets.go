package main

import (
	"embed"
	"io/fs"
)

//go:embed static
var embeddedFiles embed.FS

func staticFileSystem() (fs.FS, error) {
	return fs.Sub(embeddedFiles, "static")
}

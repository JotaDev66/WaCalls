package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var assets embed.FS

func FS() (fs.FS, error) {
	return fs.Sub(assets, "dist")
}

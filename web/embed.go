package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var staticFS embed.FS

//go:embed templates
var templateFS embed.FS

// Static returns the embedded static file system (js, css).
var Static, _ = fs.Sub(staticFS, "static")

// Templates is the embedded template filesystem.
var Templates = templateFS

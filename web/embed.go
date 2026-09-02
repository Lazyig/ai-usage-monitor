// Package web exposes the built SPA (web/dist) to the server binary.
// `npm run build` in web/ produces dist/; CI runs it before `ko build` so the
// files are embedded. Without a build only .gitkeep is present and the server
// shows a "SPA not built" notice.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built SPA rooted at dist/. Built reports whether index.html exists.
func Dist() (fsys fs.FS, built bool) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return sub, false
	}
	return sub, true
}

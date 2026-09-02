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

// Dist returns the SPA rooted at dist/. Built reports whether a real Vite build is
// present (it always emits an assets/ directory); the committed placeholder
// index.html alone does not count. The placeholder exists so `go:embed all:dist`
// always has a match even on a checkout without `npm run build`.
func Dist() (fsys fs.FS, built bool) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, false
	}
	if st, err := fs.Stat(sub, "assets"); err != nil || !st.IsDir() {
		return sub, false
	}
	return sub, true
}

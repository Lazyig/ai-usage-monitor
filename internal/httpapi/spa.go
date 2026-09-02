package httpapi

import (
	"html/template"
	"io/fs"
	"net/http"
	"strings"
)

// handleSPA serves the embedded dashboard. Real files are served as-is and every
// other path falls back to index.html so the client-side router can take over.
// /api, /downloads and /health never fall back - they answer JSON 404 instead.
func (a *api) handleSPA(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/downloads") || p == "/health" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if a.opt.SPA == nil || !a.opt.SPABuilt {
		a.serveNotBuilt(w)
		return
	}

	name := strings.TrimPrefix(p, "/")
	if name != "" && name != "index.html" {
		if st, err := fs.Stat(a.opt.SPA, name); err == nil && !st.IsDir() {
			http.FileServerFS(a.opt.SPA).ServeHTTP(w, r)
			return
		}
	}
	index, err := fs.ReadFile(a.opt.SPA, "index.html")
	if err != nil {
		a.serveNotBuilt(w)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(index)
}

func (a *api) serveNotBuilt(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if err := notBuiltHTML.Execute(w, map[string]string{"Version": a.opt.Version}); err != nil {
		a.log.Error("render placeholder", "err", err)
	}
}

var notBuiltHTML = template.Must(template.New("notbuilt").Parse(`<!doctype html>
<meta charset="utf-8">
<title>ai-usage-monitor</title>
<style>body{font:14px/1.6 system-ui,sans-serif;margin:3rem auto;max-width:36rem;padding:0 1rem}
code{background:#f4f4f5;padding:.15rem .35rem;border-radius:.25rem}</style>
<h1>ai-usage-monitor {{.Version}}</h1>
<p>The dashboard is not built into this binary. Run <code>npm ci &amp;&amp; npm run build</code>
in <code>web/</code> before <code>ko build</code> to embed <code>web/dist</code>.</p>
<p>The API is running: <a href="/health">/health</a> · <a href="/downloads/">/downloads/</a></p>
`))

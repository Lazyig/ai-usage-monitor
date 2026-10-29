package httpapi

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"ai-usage-monitor/scripts"
)

// manifest mirrors the manifest.json CI writes next to the agent binaries.
type manifest struct {
	Version string         `json:"version"`
	BuiltAt string         `json:"built_at"`
	Files   []manifestFile `json:"files"`
}

type manifestFile struct {
	Name   string `json:"name"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func (a *api) readManifest() manifest {
	m := manifest{Files: []manifestFile{}}
	if a.opt.DownloadsDir == "" {
		return m
	}
	b, err := os.ReadFile(filepath.Join(a.opt.DownloadsDir, "manifest.json"))
	if err != nil {
		return m
	}
	if err := json.Unmarshal(b, &m); err != nil {
		a.log.Error("parse manifest.json", "err", err)
		return manifest{Files: []manifestFile{}}
	}
	if m.Files == nil {
		m.Files = []manifestFile{}
	}
	return m
}

// handleDownloadIndex lists the published agent binaries: JSON by default, a
// small HTML table when the caller is a browser.
func (a *api) handleDownloadIndex(w http.ResponseWriter, r *http.Request) {
	m := a.readManifest()
	if !prefersHTML(r) {
		writeJSON(w, http.StatusOK, m)
		return
	}
	data := struct {
		manifest
		Server    string
		InstallSh string
	}{manifest: m, Server: baseURL(r), InstallSh: baseURL(r) + "/downloads/install.sh"}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := downloadsHTML.Execute(w, data); err != nil {
		a.log.Error("render downloads page", "err", err)
	}
}

func prefersHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if !strings.Contains(accept, "text/html") {
		return false
	}
	// An explicit JSON preference wins over the browser default.
	if i, j := strings.Index(accept, "application/json"), strings.Index(accept, "text/html"); i >= 0 && i < j {
		return false
	}
	return true
}

// handleDownloadFile serves one file out of the downloads directory.
func (a *api) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if a.opt.DownloadsDir == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if name == "manifest.json" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		// Revalidate every time: the dashboard compares agent versions with it,
		// and a cached copy would outlive a release.
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, os.DirFS(a.opt.DownloadsDir), "manifest.json")
		return
	}

	path := filepath.Join(a.opt.DownloadsDir, name)
	f, err := os.Open(path)
	if err != nil {
		if name == "install.sh" {
			a.serveGeneratedInstall(w, r)
			return
		}
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	switch {
	case name == "install.sh":
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	case strings.HasSuffix(name, ".txt"):
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	}
	http.ServeContent(w, r, name, st.ModTime(), f)
}

// serveGeneratedInstall renders the fallback installer when the downloads
// directory ships no install.sh of its own.
func (a *api) serveGeneratedInstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	if _, err := io.WriteString(w, scripts.UnixInstaller(baseURL(r))); err != nil {
		a.log.Error("write install.sh", "err", err)
	}
}

// baseURL reconstructs the externally visible server URL for the installer.
func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if v := r.Header.Get("X-Forwarded-Proto"); v != "" {
		scheme = strings.TrimSpace(strings.Split(v, ",")[0])
	}
	host := r.Host
	if v := r.Header.Get("X-Forwarded-Host"); v != "" {
		host = strings.TrimSpace(strings.Split(v, ",")[0])
	}
	return scheme + "://" + host
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}

var downloadsHTML = template.Must(template.New("downloads").Funcs(template.FuncMap{
	"size": humanSize,
}).Parse(`<!doctype html>
<meta charset="utf-8">
<title>ai-usage-agent downloads</title>
<style>
 body{font:14px/1.5 system-ui,sans-serif;margin:2rem;max-width:60rem}
 table{border-collapse:collapse;width:100%;margin:1rem 0}
 th,td{border-bottom:1px solid #ddd;padding:.4rem .6rem;text-align:left}
 code{background:#f4f4f5;padding:.15rem .35rem;border-radius:.25rem}
 .sha{font-family:ui-monospace,monospace;font-size:11px;color:#666;word-break:break-all}
</style>
<h1>ai-usage-agent</h1>
<p>version <code>{{.Version}}</code>{{if .BuiltAt}} · built {{.BuiltAt}}{{end}}</p>
<h2>Install</h2>
<pre><code>curl -fsSL {{.InstallSh}} | sh -s -- --server {{.Server}} --token aum_...</code></pre>
<h2>Binaries</h2>
{{if .Files}}
<table>
 <tr><th>file</th><th>os</th><th>arch</th><th>size</th><th>sha256</th></tr>
 {{range .Files}}
 <tr>
  <td><a href="/downloads/{{.Name}}">{{.Name}}</a></td>
  <td>{{.OS}}</td><td>{{.Arch}}</td><td>{{size .Size}}</td>
  <td class="sha">{{.SHA256}}</td>
 </tr>
 {{end}}
</table>
{{else}}<p>No binaries published yet.</p>{{end}}
<p><a href="/downloads/manifest.json">manifest.json</a> · <a href="/health">/health</a></p>
`))

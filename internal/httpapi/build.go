package httpapi

import (
	"net/http"
	"runtime"
	"runtime/debug"
	"strings"
)

// Build identifies the running server binary.
type Build struct {
	Version    string `json:"version"`               // release tag, 0.0.0-<sha> on main, "dev" locally
	Commit     string `json:"commit,omitempty"`      // full git revision the binary was built from
	CommitTime string `json:"commit_time,omitempty"` // RFC 3339, from the VCS
	Modified   bool   `json:"modified,omitempty"`    // built from a tree with uncommitted changes
	GoVersion  string `json:"go_version"`
}

// ReadBuild combines the ldflags version with the VCS stamp Go embeds when it
// builds inside a git checkout (CI and ko both do).
func ReadBuild(version string) Build {
	b := Build{Version: version, GoVersion: runtime.Version()}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return b
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			b.Commit = s.Value
		case "vcs.time":
			b.CommitTime = s.Value
		case "vcs.modified":
			b.Modified = strings.EqualFold(s.Value, "true")
		}
	}
	return b
}

// handleServerInfo is GET /api/v1/server: which build is running, for the
// dashboard's footer. Admin only: the commit pins the exact code an attacker
// would look at, which /health (open) does not need to reveal.
func (a *api) handleServerInfo(w http.ResponseWriter, r *http.Request) {
	b := a.opt.Build
	if b.Version == "" {
		b.Version = a.opt.Version
	}
	writeJSON(w, http.StatusOK, b)
}

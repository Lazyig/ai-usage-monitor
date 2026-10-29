// Package httpapi implements the server side of docs/API.md: the ingest
// endpoint agents push to, the admin query endpoints the dashboard reads, the
// binary download directory and the embedded SPA.
package httpapi

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"ai-usage-monitor/internal/store"
)

// MaxBodyBytes caps every request body (a 5,000 event batch is ~2 MiB).
const MaxBodyBytes = 8 << 20

// MaxBatchEvents is the ingest batch limit from docs/API.md.
const MaxBatchEvents = 5000

// DefaultTZ is the timezone day buckets use when the caller does not pass one.
const DefaultTZ = "Asia/Seoul"

// Options configures New.
type Options struct {
	AdminToken   string // empty => admin routes answer 503
	Version      string // reported by /health
	Build        Build  // reported to the dashboard by GET /api/v1/server
	DownloadsDir string // directory holding agent binaries + manifest.json
	// PublicURL, when set, is the server's external base URL (https://usage.example.com).
	// The downloads page and the generated install.sh use it instead of the
	// request's Host and X-Forwarded-* headers, which any client can set.
	PublicURL string
	SPA       fs.FS // built SPA (web.Dist())
	SPABuilt  bool  // false => "/" serves the "not built" notice
	Logger    *slog.Logger
	// Draining, when set by the process on SIGTERM, makes readiness fail so the
	// load balancer stops sending new traffic before the listener closes.
	Draining *atomic.Bool
}

type api struct {
	store   *store.Store
	pricing *Pricing
	opt     Options
	log     *slog.Logger

	mu       sync.Mutex
	devCache map[string]cachedDevice // token hash -> device
	// devGen counts forgetDevice calls and forgotAt records the count at which
	// each device was last forgotten. A lookup that raced a forget of its own
	// device (read it before a revoke committed) must not cache what it read.
	// Every ingest forgets its device, so the check is per device.
	devGen   uint64
	forgotAt map[string]uint64

	counts   store.Counts // /health's cached row counts, see cachedCounts
	countsAt time.Time

	pingLoggedAt time.Time // last "database check failed" log line
}

// New builds the complete handler tree.
func New(st *store.Store, pr *Pricing, opt Options) http.Handler {
	if opt.Logger == nil {
		opt.Logger = slog.Default()
	}
	a := &api{store: st, pricing: pr, opt: opt, log: opt.Logger, devCache: map[string]cachedDevice{}, forgotAt: map[string]uint64{}}

	mux := http.NewServeMux()

	// Collection (device token).
	mux.Handle("POST /api/v1/ingest", a.device(http.HandlerFunc(a.handleIngest)))
	mux.Handle("GET /api/v1/agent/whoami", a.device(http.HandlerFunc(a.handleWhoAmI)))

	// Queries and administration (admin token).
	mux.Handle("GET /api/v1/summary", a.admin(http.HandlerFunc(a.handleSummary)))
	mux.Handle("GET /api/v1/timeseries", a.admin(http.HandlerFunc(a.handleTimeseries)))
	mux.Handle("GET /api/v1/breakdown", a.admin(http.HandlerFunc(a.handleBreakdown)))
	mux.Handle("GET /api/v1/throughput", a.admin(http.HandlerFunc(a.handleThroughput)))
	mux.Handle("GET /api/v1/events", a.admin(http.HandlerFunc(a.handleEvents)))
	mux.Handle("GET /api/v1/devices", a.admin(http.HandlerFunc(a.handleListDevices)))
	mux.Handle("POST /api/v1/devices", a.admin(http.HandlerFunc(a.handleCreateDevice)))
	mux.Handle("DELETE /api/v1/devices/{id}", a.admin(http.HandlerFunc(a.handleDeleteDevice)))
	mux.Handle("PATCH /api/v1/devices/{id}", a.admin(http.HandlerFunc(a.handleRenameDevice)))
	mux.Handle("POST /api/v1/devices/{id}/rotate", a.admin(http.HandlerFunc(a.handleRotateDevice)))
	mux.Handle("DELETE /api/v1/devices/{id}/events", a.admin(http.HandlerFunc(a.handlePurgeDeviceEvents)))
	mux.Handle("POST /api/v1/devices/{id}/resync", a.admin(http.HandlerFunc(a.handleResyncDevice)))
	mux.Handle("GET /api/v1/server", a.admin(http.HandlerFunc(a.handleServerInfo)))
	mux.Handle("GET /api/v1/pricing", a.admin(http.HandlerFunc(a.handlePricing)))
	mux.Handle("POST /api/v1/pricing/reload", a.admin(http.HandlerFunc(a.handlePricingReload)))

	// Open endpoints.
	mux.HandleFunc("GET /health", a.handleHealth)
	mux.HandleFunc("GET /health/live", a.handleLive)
	mux.HandleFunc("GET /health/ready", a.handleReady)
	mux.HandleFunc("GET /api/v1/health", a.handleHealth)
	mux.HandleFunc("GET /downloads", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/downloads/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /downloads/{$}", a.handleDownloadIndex)
	mux.HandleFunc("GET /downloads/{name}", a.handleDownloadFile)
	mux.HandleFunc("HEAD /downloads/{name}", a.handleDownloadFile)

	// Everything else is the SPA.
	mux.HandleFunc("/", a.handleSPA)

	return a.logging(securityHeaders(noStoreAPI(limitBody(mux))))
}

// --- responses -------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

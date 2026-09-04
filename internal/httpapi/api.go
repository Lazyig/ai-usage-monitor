// Package httpapi implements the server side of docs/API.md: the ingest
// endpoint agents push to, the admin query endpoints the dashboard reads, the
// binary download directory and the embedded SPA.
package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
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

// deviceTokenTTL bounds how long a device token stays cached; revocation through
// this server invalidates the entry immediately, the TTL only covers changes
// made by another process.
const deviceTokenTTL = 60 * time.Second

// Options configures New.
type Options struct {
	AdminToken   string // empty => admin routes answer 503
	Version      string // reported by /health
	DownloadsDir string // directory holding agent binaries + manifest.json
	SPA          fs.FS  // built SPA (web.Dist())
	SPABuilt     bool   // false => "/" serves the "not built" notice
	Logger       *slog.Logger
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
}

type cachedDevice struct {
	dev store.Device
	at  time.Time
}

// New builds the complete handler tree.
func New(st *store.Store, pr *Pricing, opt Options) http.Handler {
	if opt.Logger == nil {
		opt.Logger = slog.Default()
	}
	a := &api{store: st, pricing: pr, opt: opt, log: opt.Logger, devCache: map[string]cachedDevice{}}

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

	return a.logging(noStoreAPI(limitBody(mux)))
}

// --- middleware ------------------------------------------------------------

func limitBody(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
		}
		h.ServeHTTP(w, r)
	})
}

// noStoreAPI keeps API answers out of every cache; the dashboard polls them.
func noStoreAPI(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/health" || strings.HasPrefix(r.URL.Path, "/health/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		h.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func (a *api) logging(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		h.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"bytes", sw.bytes,
			"duration", time.Since(start).Round(time.Microsecond).String(),
		}
		if d, ok := deviceFrom(r.Context()); ok {
			attrs = append(attrs, "device", d.ID)
		}
		level := slog.LevelInfo
		if sw.status >= 500 {
			level = slog.LevelError
		}
		a.log.Log(r.Context(), level, "http", attrs...)
	})
}

// bearer extracts the Authorization bearer token.
func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) < 7 || !strings.EqualFold(h[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}

// admin guards the query/administration routes.
func (a *api) admin(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.opt.AdminToken == "" {
			writeError(w, http.StatusServiceUnavailable, "admin token not configured")
			return
		}
		tok := bearer(r)
		if subtle.ConstantTimeCompare([]byte(tok), []byte(a.opt.AdminToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		h.ServeHTTP(w, r)
	})
}

type ctxKey int

const deviceKey ctxKey = 1

func deviceFrom(ctx context.Context) (store.Device, bool) {
	d, ok := ctx.Value(deviceKey).(store.Device)
	return d, ok
}

// device guards ingest: the bearer token must hash to a live device.
func (a *api) device(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := bearer(r)
		if tok == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		dev, err := a.lookupDevice(r.Context(), store.HashToken(tok))
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				a.log.Error("device lookup", "err", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if dev.RevokedAt != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), deviceKey, dev)))
	})
}

func (a *api) lookupDevice(ctx context.Context, hash string) (store.Device, error) {
	a.mu.Lock()
	if c, ok := a.devCache[hash]; ok && time.Since(c.at) < deviceTokenTTL {
		a.mu.Unlock()
		return c.dev, nil
	}
	a.mu.Unlock()

	dev, err := a.store.DeviceByTokenHash(ctx, hash)
	if err != nil {
		return store.Device{}, err
	}
	a.mu.Lock()
	a.devCache[hash] = cachedDevice{dev: dev, at: time.Now()}
	a.mu.Unlock()
	return dev, nil
}

// forgetDevice drops cached tokens for a device so a revocation takes effect now.
func (a *api) forgetDevice(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for hash, c := range a.devCache {
		if c.dev.ID == id {
			delete(a.devCache, hash)
		}
	}
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

// boolParam reads a flag query parameter: "true", "1", "yes" and "on" (any
// case) enable it, anything else - including a missing parameter - does not.
func boolParam(r *http.Request, name string) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get(name))) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

func intParam(r *http.Request, name string, def int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

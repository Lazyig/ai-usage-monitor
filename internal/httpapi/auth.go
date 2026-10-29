package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"ai-usage-monitor/internal/store"
)

// deviceTokenTTL bounds how long a device token stays cached; revocation through
// this server invalidates the entry immediately, the TTL only covers changes
// made by another process.
const deviceTokenTTL = 60 * time.Second

type cachedDevice struct {
	dev store.Device
	at  time.Time
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
	gen := a.devGen
	a.mu.Unlock()

	dev, err := a.store.DeviceByTokenHash(ctx, hash)
	if err != nil {
		return store.Device{}, err
	}
	a.mu.Lock()
	if a.forgotAt[dev.ID] <= gen {
		a.devCache[hash] = cachedDevice{dev: dev, at: time.Now()}
	}
	a.mu.Unlock()
	return dev, nil
}

// forgetDevice drops cached tokens for a device so a revocation takes effect now.
func (a *api) forgetDevice(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.devGen++
	a.forgotAt[id] = a.devGen
	for hash, c := range a.devCache {
		if c.dev.ID == id {
			delete(a.devCache, hash)
		}
	}
}

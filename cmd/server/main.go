// Command server ingests usage events from agents, stores them in SQLite and serves the API + SPA.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	// The image is distroless/static and has no /usr/share/zoneinfo, but every
	// day bucket is computed in an IANA zone (default Asia/Seoul).
	_ "time/tzdata"

	"ai-usage-monitor/internal/httpapi"
	"ai-usage-monitor/internal/store"
	"ai-usage-monitor/web"
)

var version = "dev"

func main() {
	addr := flag.String("addr", envOr("ADDR", ":8080"), "listen address")
	dbPath := flag.String("db", envOr("DB_PATH", "data/usage.db"), "sqlite file")
	pricingPath := flag.String("pricing", envOr("PRICING_PATH", "pricing.yaml"), "pricing table")
	downloadsDir := flag.String("downloads", envOr("DOWNLOADS_DIR", defaultDownloadsDir()), "directory served at /downloads/")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" {
		log.Warn("ADMIN_TOKEN not set; every admin route will answer 503")
	}

	pricingProvider, err := httpapi.NewPricing(*pricingPath)
	if err != nil {
		log.Error("load pricing", "err", err)
		os.Exit(1)
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		log.Error("open store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	spa, spaBuilt := web.Dist()
	log.Info("ai-usage-server starting",
		"version", version,
		"addr", *addr,
		"db", *dbPath,
		"pricing", *pricingPath,
		"rates", len(pricingProvider.Rates()),
		"downloads", *downloadsDir,
		"spa_built", spaBuilt,
		"admin_token_set", adminToken != "",
	)

	var draining atomic.Bool
	handler := httpapi.New(st, pricingProvider, httpapi.Options{
		Draining:     &draining,
		AdminToken:   adminToken,
		Version:      version,
		DownloadsDir: *downloadsDir,
		SPA:          spa,
		SPABuilt:     spaBuilt,
		Logger:       log,
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// k8s sends SIGTERM; drain in-flight requests before the SQLite handle closes.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		log.Error("listen", "err", err)
		os.Exit(1)
	case <-ctx.Done():
		// Fail readiness first and give the endpoints controller / load balancer
		// time to stop routing to this pod; only then close the listener. Without
		// the pause a rolling update still drops the requests in flight to the
		// old pod's address.
		draining.Store(true)
		delay := shutdownDelay()
		log.Info("shutting down", "drain_delay", delay)
		select {
		case <-time.After(delay):
		case <-errc:
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
	}
}

// defaultDownloadsDir is the ko data directory in the image, the repo path locally.
func defaultDownloadsDir() string {
	if kd := os.Getenv("KO_DATA_PATH"); kd != "" {
		return filepath.Join(kd, "downloads")
	}
	return filepath.Join("cmd", "server", "kodata", "downloads")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// shutdownDelay is how long readiness stays red before the listener closes
// (SHUTDOWN_DELAY, default 8s; k8s endpoint propagation is usually 1-3s).
func shutdownDelay() time.Duration {
	if v := os.Getenv("SHUTDOWN_DELAY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			return d
		}
	}
	return 8 * time.Second
}

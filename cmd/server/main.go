// Command server ingests usage events from agents, stores them in SQLite or
// PostgreSQL and serves the API + SPA.
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
	"strings"
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
	// No default shown here: it would print a URL with its password in -h.
	dbFlag := flag.String("db", "", "database: a SQLite file path or a postgres:// URL (env DB_PATH, then DATABASE_URL; default "+defaultDBPath+")")
	pricingPath := flag.String("pricing", envOr("PRICING_PATH", "pricing.yaml"), "pricing table")
	downloadsDir := flag.String("downloads", envOr("DOWNLOADS_DIR", defaultDownloadsDir()), "directory served at /downloads/")
	publicURLFlag := flag.String("public-url", envOr("PUBLIC_URL", ""), "external base URL for install commands, e.g. https://usage.example.com (default: taken from each request)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	dbFlagSet := false
	flag.Visit(func(f *flag.Flag) { dbFlagSet = dbFlagSet || f.Name == "db" })

	build := httpapi.ReadBuild(version)
	if *showVersion {
		fmt.Println(version, orDash(build.Commit))
		return
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	publicURL, err := httpapi.ParsePublicURL(*publicURLFlag)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(2)
	}
	if strings.HasPrefix(publicURL, "http://") {
		log.Warn("PUBLIC_URL is plain http: agents send their device token unencrypted and refuse it unless installed with --allow-http (loopback excepted)")
	}

	db, err := resolveDB(*dbFlag, dbFlagSet, os.Getenv, fileExists)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(2)
	}
	for _, n := range db.notes {
		log.Warn(n)
	}
	conns, err := maxConns(os.Getenv)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(2)
	}

	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" {
		log.Warn("ADMIN_TOKEN not set; every admin route will answer 503")
	}

	pricingProvider, err := httpapi.NewPricing(*pricingPath)
	if err != nil {
		log.Error("load pricing", "err", err)
		os.Exit(1)
	}

	// k8s sends SIGTERM: it ends the wait for the database at startup, and
	// later drains in-flight requests before the database handle closes.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	backend := "sqlite"
	if store.IsPostgresURL(db.dsn) {
		backend = "postgres"
	}
	for _, w := range store.PostgresWarnings(db.dsn) {
		log.Warn(w)
	}
	st, err := openStore(ctx, log, db.dsn, store.Options{MaxConns: conns})
	if err != nil {
		log.Error("open store", "backend", backend, "db", store.RedactDSN(db.dsn), "err", err)
		os.Exit(1)
	}
	defer st.Close()

	spa, spaBuilt := web.Dist()
	log.Info("ai-usage-server starting",
		"version", version,
		"commit", build.Commit,
		"addr", *addr,
		"db_backend", backend,
		"db", store.RedactDSN(db.dsn),
		"db_source", db.source,
		"pricing", *pricingPath,
		"rates", len(pricingProvider.Rates()),
		"downloads", *downloadsDir,
		"public_url", publicURL,
		"spa_built", spaBuilt,
		"admin_token_set", adminToken != "",
	)

	var draining atomic.Bool
	handler := httpapi.New(st, pricingProvider, httpapi.Options{
		Draining:     &draining,
		AdminToken:   adminToken,
		Version:      version,
		Build:        build,
		DownloadsDir: *downloadsDir,
		PublicURL:    publicURL,
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

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

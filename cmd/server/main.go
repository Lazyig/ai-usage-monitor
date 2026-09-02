// Command server ingests usage events from agents, stores them in SQLite and serves the API + SPA.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"ai-usage-monitor/internal/pricing"
)

var version = "dev"

func main() {
	addr := flag.String("addr", envOr("ADDR", ":8080"), "listen address")
	dbPath := flag.String("db", envOr("DB_PATH", "data/usage.db"), "sqlite file")
	pricingPath := flag.String("pricing", envOr("PRICING_PATH", "pricing.yaml"), "pricing table")
	flag.Parse()

	if _, err := pricing.Load(*pricingPath); err != nil {
		log.Fatalf("load pricing: %v", err)
	}
	if os.Getenv("ADMIN_TOKEN") == "" {
		log.Println("warning: ADMIN_TOKEN not set; query API will be unreachable")
	}
	log.Printf("ai-usage-server %s addr=%s db=%s (Phase 2: http server not implemented yet)", version, *addr, *dbPath)
	fmt.Println("TODO Phase 2")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"ai-usage-monitor/internal/event"
	"ai-usage-monitor/internal/store"
)

func TestServiceTierIngestBackfillAndQueries(t *testing.T) {
	h := newHarness(t, Options{AdminToken: adminToken})
	id, token := h.newDevice("codex")
	ts := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)
	for i, tier := range []string{"", "default", "priority", "fast"} {
		e := sampleEvent(fmt.Sprint("tier", i), id, ts, 100, 10)
		e.Source, e.ServiceTier = event.SourceCodex, tier
		h.ingest(id, token, e)
	}
	base := "?from=2026-11-16&to=2026-11-17&tz=UTC&source=codex&service_tier=priority"
	for _, path := range []string{"summary", "breakdown", "timeseries", "throughput", "events"} {
		t.Run(path, func(t *testing.T) {
			w := h.do(http.MethodGet, "/api/v1/"+path+base+"&group_by=service_tier", adminToken, nil)
			if w.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", path, w.Code, w.Body)
			}
			var payload struct {
				Totals store.Totals          `json:"totals"`
				Total  store.ThroughputGroup `json:"total"`
				Rows   []store.Group         `json:"rows"`
				Series []store.Series        `json:"series"`
				Events []store.EventRow      `json:"events"`
			}
			h.decode(w, &payload)
			var count int64
			switch path {
			case "summary":
				count = payload.Totals.Events
			case "throughput":
				count = payload.Total.Events
			case "breakdown":
				if len(payload.Rows) == 1 && payload.Rows[0].Key == "fast" {
					count = payload.Rows[0].Events
				}
			case "timeseries":
				if len(payload.Series) == 1 && payload.Series[0].Key == "fast" && len(payload.Series[0].Points) == 1 {
					count = payload.Series[0].Points[0].Events
				}
			case "events":
				for _, e := range payload.Events {
					if e.ServiceTier == "fast" {
						count++
					}
				}
			}
			if count != 2 {
				t.Fatalf("tier filter/group did not apply: %+v", payload)
			}
		})
	}
	w := h.do(http.MethodGet, "/api/v1/events"+strings.ReplaceAll(base, "priority", "unknown"), adminToken, nil)
	var before store.EventPage
	h.decode(w, &before)
	if len(before.Events) != 1 || before.Events[0].ServiceTier != "" {
		t.Fatalf("unknown events: %+v", before)
	}
	e := before.Events[0].UsageEvent
	e.ServiceTier = "fast"
	res := h.ingest(id, token, e)
	if res.Updated != 1 || res.Accepted != 0 {
		t.Fatalf("metadata-only backfill: %+v", res)
	}
	w = h.do(http.MethodGet, "/api/v1/events"+base, adminToken, nil)
	var after store.EventPage
	h.decode(w, &after)
	if len(after.Events) != 3 {
		t.Fatalf("backfilled fast events: %+v", after)
	}
}

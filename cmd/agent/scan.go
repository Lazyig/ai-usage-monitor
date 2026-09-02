package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"ai-usage-monitor/internal/agentcfg"
	"ai-usage-monitor/internal/event"
)

// cmdScan is the offline validation tool: it reads every enabled collector from
// scratch and prints what it found. It never touches the persisted cursors and
// never contacts the server, so it is safe to run at any time, and it works on
// a machine that has no config file at all.
func cmdScan(args []string) error {
	fs := newFlagSet("scan")
	var (
		since    = fs.String("since", "7d", "only report events newer than this (e.g. 24h, 7d, 4w, or 'all')")
		sources  = fs.String("source", "", "comma separated sources to scan (default: all enabled)")
		asJSON   = fs.Bool("json", false, "dump the raw events as JSON instead of the tables")
		stateDir = fs.String("state-dir", "", "state directory (default: $AI_USAGE_STATE_DIR or the per-OS config dir)")
		days     = fs.Int("days", 14, "how many days the per-day table shows (0 = all)")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: ai-usage-agent scan [flags]\n\nRead the local logs and print a usage summary. No server required.\n\nflags:\n")
		fs.PrintDefaults()
	}
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return usagef("scan takes no positional arguments (got %q)", fs.Arg(0))
	}

	cutoff, err := parseSince(*since)
	if err != nil {
		return &usageError{err: err}
	}
	sel, err := parseSources(*sources)
	if err != nil {
		return &usageError{err: err}
	}

	dir := resolveStateDir(*stateDir)
	cfg, _, err := agentcfg.Load(dir)
	if err != nil {
		return err
	}

	ctx, stop := signalContext()
	defer stop()

	collectors := build(cfg, sel)
	if len(collectors) == 0 {
		return fmt.Errorf("no collectors enabled (available: %s)", strings.Join(sourceNames(), ", "))
	}

	var (
		all     []event.UsageEvent
		scanned []scanResult
	)
	for _, a := range collectors {
		start := time.Now()
		// A nil cursor means "everything": scan is a full, read-only pass.
		events, _, err := a.Collector.Scan(ctx, cfg.DeviceID, nil)
		res := scanResult{Source: a.Source, Root: a.Root, Took: time.Since(start), Err: err}
		for _, e := range events {
			if !cutoff.IsZero() && e.Timestamp.Before(cutoff) {
				res.Skipped++
				continue
			}
			all = append(all, e)
			res.Kept++
		}
		scanned = append(scanned, res)
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	sort.Slice(all, func(i, j int) bool { return all[i].Timestamp.Before(all[j].Timestamp) })

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(all); err != nil {
			return err
		}
		return firstScanError(scanned)
	}

	printScan(stdout, cfg, scanned, all, *since, *days)
	return firstScanError(scanned)
}

// scanResult is the per-collector outcome shown above the tables.
type scanResult struct {
	Source  event.Source
	Root    string
	Kept    int
	Skipped int
	Took    time.Duration
	Err     error
}

func firstScanError(res []scanResult) error {
	for _, r := range res {
		if r.Err != nil {
			return fmt.Errorf("%s: %w", r.Source, r.Err)
		}
	}
	return nil
}

func printScan(w io.Writer, cfg agentcfg.Config, res []scanResult, events []event.UsageEvent, since string, days int) {
	fmt.Fprintf(w, "device %s (%s)  since=%s  tz=%s\n\n", cfg.DeviceName, cfg.DeviceID, since, time.Now().Location())

	for _, r := range res {
		status := fmt.Sprintf("%s events", num(int64(r.Kept)))
		if r.Skipped > 0 {
			status += fmt.Sprintf(" (%s older than the window)", num(int64(r.Skipped)))
		}
		if r.Err != nil {
			status = "ERROR: " + r.Err.Error()
		} else if !dirExists(r.Root) {
			status = "not installed (no " + r.Root + ")"
		}
		fmt.Fprintf(w, "  %-13s %-55s %s  [%s]\n", r.Source, r.Root, status, r.Took.Round(time.Millisecond))
	}
	fmt.Fprintln(w)

	if len(events) == 0 {
		fmt.Fprintln(w, "no events in this window.")
		return
	}

	rep := newReport()
	for _, e := range events {
		rep.add(e)
	}
	rep.writeModelTable(w)
	fmt.Fprintf(w, "\nper day (last %s, local time)\n", pluralDays(days))
	rep.writeDayTable(w, days)
	fmt.Fprintf(w, "\n%s events from %s to %s\n",
		num(int64(rep.grand.Calls)), rep.first.Format(time.RFC3339), rep.last.Format(time.RFC3339))
}

func pluralDays(n int) string {
	if n <= 0 {
		return "all days"
	}
	return strconv.Itoa(n) + " days"
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// parseSince accepts a Go duration ("90m", "12h"), the day/week shorthands
// ("7d", "4w") the CLI advertises, or "all"/"0" for no cutoff. It returns the
// oldest timestamp to keep, zero for "everything".
func parseSince(s string) (time.Time, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "all" || s == "0" {
		return time.Time{}, nil
	}
	var d time.Duration
	switch unit := s[len(s)-1]; unit {
	case 'd', 'w':
		n, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid --since %q: %w", s, err)
		}
		mult := 24 * float64(time.Hour)
		if unit == 'w' {
			mult *= 7
		}
		d = time.Duration(n * mult)
	default:
		var err error
		d, err = time.ParseDuration(s)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid --since %q: want a duration like 24h, 7d, 4w or 'all'", s)
		}
	}
	if d <= 0 {
		return time.Time{}, nil
	}
	return time.Now().Add(-d), nil
}

// resolveStateDir lets --state-dir win over $AI_USAGE_STATE_DIR and the per-OS
// default, matching the "flags override env" rule.
func resolveStateDir(flagValue string) string {
	if strings.TrimSpace(flagValue) != "" {
		return flagValue
	}
	return agentcfg.StateDir()
}

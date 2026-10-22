package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"ai-usage-monitor/internal/agentcfg"
	"ai-usage-monitor/internal/event"
)

// totals is one aggregation bucket. CacheWrite folds the Claude 5m/1h split
// back together: the agent's local report is about volume, and the split only
// matters for pricing, which happens on the server.
type totals struct {
	Calls      int
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	Reasoning  int64
}

func (t *totals) add(e event.UsageEvent) {
	t.Calls++
	t.Input += e.Input
	t.Output += e.Output
	t.CacheRead += e.CacheRead
	t.CacheWrite += e.CacheWrite + e.CacheWrite5m + e.CacheWrite1h
	t.Reasoning += e.Reasoning
}

// merge folds another bucket in, for the subtotal and TOTAL rows.
func (t *totals) merge(o totals) {
	t.Calls += o.Calls
	t.Input += o.Input
	t.Output += o.Output
	t.CacheRead += o.CacheRead
	t.CacheWrite += o.CacheWrite
	t.Reasoning += o.Reasoning
}

func (t totals) tokens() int64 {
	return t.Input + t.Output + t.CacheRead + t.CacheWrite
}

type modelKey struct {
	Source event.Source
	Model  string
}

// report accumulates everything the scan table needs.
type report struct {
	byModel map[modelKey]*totals
	byDay   map[string]*totals // local calendar date -> totals
	grand   totals
	first   time.Time
	last    time.Time
}

func newReport() *report {
	return &report{byModel: map[modelKey]*totals{}, byDay: map[string]*totals{}}
}

func (r *report) add(e event.UsageEvent) {
	k := modelKey{Source: e.Source, Model: e.Model}
	if k.Model == "" {
		k.Model = "(unknown)"
	}
	if r.byModel[k] == nil {
		r.byModel[k] = &totals{}
	}
	r.byModel[k].add(e)

	day := e.Timestamp.Local().Format("2006-01-02")
	if r.byDay[day] == nil {
		r.byDay[day] = &totals{}
	}
	r.byDay[day].add(e)

	r.grand.add(e)
	local := e.Timestamp.Local()
	if r.first.IsZero() || local.Before(r.first) {
		r.first = local
	}
	if local.After(r.last) {
		r.last = local
	}
}

// writeModelTable prints one row per source and model, sorted by source and
// then by descending token volume, with a subtotal per source.
func (r *report) writeModelTable(w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprint(tw, "SOURCE\tMODEL\tCALLS\tINPUT\tOUTPUT\tCACHE READ\tCACHE WRITE\tREASONING\t\n")

	keys := make([]modelKey, 0, len(r.byModel))
	for k := range r.byModel {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Source != keys[j].Source {
			return sourceRank(keys[i].Source) < sourceRank(keys[j].Source)
		}
		ti, tj := r.byModel[keys[i]].tokens(), r.byModel[keys[j]].tokens()
		if ti != tj {
			return ti > tj
		}
		return keys[i].Model < keys[j].Model
	})

	var (
		cur event.Source
		sub totals
		n   int
	)
	flushSub := func() {
		if n > 1 {
			writeRow(tw, "", "  (subtotal)", sub)
		}
		sub, n = totals{}, 0
	}
	for _, k := range keys {
		if k.Source != cur {
			flushSub()
			cur = k.Source
		}
		t := *r.byModel[k]
		writeRow(tw, string(k.Source), k.Model, t)
		sub.merge(t)
		n++
	}
	flushSub()
	if len(keys) > 0 {
		writeRow(tw, "TOTAL", "", r.grand)
	}
	tw.Flush()
}

// writeDayTable prints the last n calendar days that carry usage, oldest first.
// The TOTAL row sums the rows actually shown, and older days that were cut off
// are called out so the number is never mistaken for the grand total.
func (r *report) writeDayTable(w io.Writer, n int) {
	days := make([]string, 0, len(r.byDay))
	for d := range r.byDay {
		days = append(days, d)
	}
	sort.Strings(days)
	hidden := 0
	if n > 0 && len(days) > n {
		hidden = len(days) - n
		days = days[hidden:]
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprint(tw, "DATE\tCALLS\tINPUT\tOUTPUT\tCACHE READ\tCACHE WRITE\tREASONING\t\n")
	var shown totals
	for _, d := range days {
		t := *r.byDay[d]
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t\n", d,
			num(int64(t.Calls)), num(t.Input), num(t.Output), num(t.CacheRead), num(t.CacheWrite), num(t.Reasoning))
		shown.merge(t)
	}
	if len(days) > 0 {
		fmt.Fprintf(tw, "TOTAL\t%s\t%s\t%s\t%s\t%s\t%s\t\n",
			num(int64(shown.Calls)), num(shown.Input), num(shown.Output),
			num(shown.CacheRead), num(shown.CacheWrite), num(shown.Reasoning))
	}
	tw.Flush()
	if hidden > 0 {
		fmt.Fprintf(w, "(%d earlier day(s) in the window are not shown; use --days 0 for all)\n", hidden)
	}
}

func writeRow(tw io.Writer, source, model string, t totals) {
	fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t\n", source, model,
		num(int64(t.Calls)), num(t.Input), num(t.Output), num(t.CacheRead), num(t.CacheWrite), num(t.Reasoning))
}

// sourceRank keeps the tables in the canonical source order.
func sourceRank(s event.Source) int {
	for i, k := range known() {
		if k == s {
			return i
		}
	}
	return 99
}

// num formats a token count with thousands separators.
func num(v int64) string {
	s := strconv.FormatInt(v, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
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
		} else if !anyDirExists(r.Roots) {
			status = "not installed (no " + strings.Join(r.Roots, " or ") + ")"
		}
		fmt.Fprintf(w, "  %-13s %-55s %s  [%s]\n", r.Source, strings.Join(r.Roots, ", "), status, r.Took.Round(time.Millisecond))
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

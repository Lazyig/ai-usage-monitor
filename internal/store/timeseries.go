// This file implements GET /timeseries: the same aggregate as /summary, bucketed
// by local day or hour.

package store

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Point is one bucket of a series.
type Point struct {
	T          string  `json:"t"`
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	CacheRead  int64   `json:"cache_read"`
	CacheWrite int64   `json:"cache_write"`
	Reasoning  int64   `json:"reasoning"`
	CostUSD    float64 `json:"cost_usd"`
	Events     int64   `json:"events"`
}

// Series is one group's points, oldest first.
type Series struct {
	Key    string  `json:"key"`
	Name   string  `json:"name,omitempty"`
	Points []Point `json:"points"`
}

// Timeseries is GET /timeseries.
type Timeseries struct {
	Bucket  string   `json:"bucket"`
	GroupBy string   `json:"group_by"`
	TZ      string   `json:"tz"`
	Series  []Series `json:"series"`
}

type tzSegment struct {
	startMs, endMs int64
	offsetSec      int64
}

// bucketSegments splits [from,to) into maximal runs of buckets that share one
// UTC offset, using the offset in effect at each bucket's *start*. Inside a
// segment, (ts+offset)/bucketMs assigns every event to the right local bucket,
// including across a DST change (the transition simply ends the segment).
func bucketSegments(loc *time.Location, from, to time.Time, bucket string) []tzSegment {
	if !from.Before(to) {
		return nil
	}
	var segs []tzSegment
	cur := bucketStart(from.In(loc), bucket)
	fromMs, toMs := toMillis(from), toMillis(to)
	for i := 0; cur.Before(to) && i < 1_000_000; i++ {
		_, off := cur.Zone()
		next := nextBucket(cur, bucket)
		if !next.After(cur) { // paranoia: never loop forever
			break
		}
		if n := len(segs); n > 0 && segs[n-1].offsetSec == int64(off) && segs[n-1].endMs == cur.UnixMilli() {
			segs[n-1].endMs = next.UnixMilli()
		} else {
			segs = append(segs, tzSegment{startMs: cur.UnixMilli(), endMs: next.UnixMilli(), offsetSec: int64(off)})
		}
		cur = next
	}
	for i := range segs {
		if segs[i].startMs < fromMs {
			segs[i].startMs = fromMs
		}
		if segs[i].endMs > toMs {
			segs[i].endMs = toMs
		}
	}
	return segs
}

func bucketStart(t time.Time, bucket string) time.Time {
	y, m, d := t.Date()
	if bucket == "hour" {
		return time.Date(y, m, d, t.Hour(), 0, 0, 0, t.Location())
	}
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func nextBucket(t time.Time, bucket string) time.Time {
	if bucket == "hour" {
		return bucketStart(t.Add(time.Hour), "hour")
	}
	y, m, d := t.Date()
	// Noon on the next calendar day, then back to that day's start: immune to
	// zones where local midnight does not exist on a DST day.
	return bucketStart(time.Date(y, m, d+1, 12, 0, 0, 0, t.Location()), "day")
}

// Timeseries buckets the range by day or hour in loc and groups by one dimension.
func (s *Store) Timeseries(ctx context.Context, f Filter, bucket, groupBy string, loc *time.Location, p Pricer) (Timeseries, error) {
	col, ok := GroupColumn(groupBy)
	if !ok {
		return Timeseries{}, fmt.Errorf("store: unsupported group_by %q", groupBy)
	}
	bucketMs := dayMs
	layout := "2006-01-02"
	switch bucket {
	case "day", "":
		bucket = "day"
	case "hour":
		bucketMs = hourMs
		layout = time.RFC3339
	default:
		return Timeseries{}, fmt.Errorf("store: unsupported bucket %q", bucket)
	}
	names, err := s.deviceNames(ctx)
	if err != nil {
		return Timeseries{}, err
	}

	// series key -> bucket start (unix millis) -> the folded rows of that bucket.
	series := map[string]map[int64]*acc{}
	var keyOrd []string

	for _, seg := range bucketSegments(loc, f.From, f.To, bucket) {
		if seg.startMs >= seg.endMs {
			continue
		}
		sf := f
		sf.From = fromMillis(seg.startMs)
		sf.To = fromMillis(seg.endMs)
		rows, err := s.aggregate(ctx, sf, col, bucketMs, seg.offsetSec, thresholdsOf(p))
		if err != nil {
			return Timeseries{}, err
		}
		for _, r := range rows {
			// The segment's offset is the one at its buckets' start. On a
			// fall-back day the last local hour still maps one bucket on, so
			// the key is taken from the local calendar, not the arithmetic.
			startMs := bucketStart(fromMillis(r.bucket*bucketMs-seg.offsetSec*1000).In(loc), bucket).UnixMilli()
			usd, known, est := priceRow(p, r)
			m, ok := series[r.key]
			if !ok {
				m = map[int64]*acc{}
				series[r.key] = m
				keyOrd = append(keyOrd, r.key)
			}
			c, ok := m[startMs]
			if !ok {
				c = &acc{}
				m[startMs] = c
			}
			c.add(r, usd, known, est)
		}
	}

	out := Timeseries{Bucket: bucket, GroupBy: groupBy, TZ: loc.String(), Series: []Series{}}
	sort.Strings(keyOrd)
	for _, key := range keyOrd {
		cells := series[key]
		starts := make([]int64, 0, len(cells))
		for k := range cells {
			starts = append(starts, k)
		}
		sort.Slice(starts, func(i, j int) bool { return starts[i] < starts[j] })
		ser := Series{Key: key, Points: make([]Point, 0, len(starts))}
		if groupBy == "device" {
			ser.Name = names[key]
		}
		for _, st := range starts {
			ser.Points = append(ser.Points, cells[st].point(time.UnixMilli(st).In(loc).Format(layout)))
		}
		out.Series = append(out.Series, ser)
	}
	return out, nil
}

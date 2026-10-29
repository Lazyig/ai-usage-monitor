package store

import (
	"context"
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"ai-usage-monitor/internal/event"
)

// referenceInsert is InsertEvents as it was before batching: one event at a
// time, in batch order. The batched version must match it exactly.
func referenceInsert(ctx context.Context, sqlTx *sql.Tx, evs []event.UsageEvent, received int64) (accepted, duplicates, updated int, err error) {
	tx := &stmtTx{tx: sqlTx}
	for _, e := range evs {
		e = cleanEvent(e)
		res, err := tx.exec(ctx, `INSERT INTO usage_events(`+eventCols+`)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) ON CONFLICT(event_id) DO NOTHING`,
			e.EventID, e.DeviceID, string(e.Source), toMillis(e.Timestamp), e.Model, e.SessionID, e.Project,
			e.Input, e.Output, e.CacheRead, e.CacheWrite5m, e.CacheWrite1h, e.CacheWrite, e.Reasoning,
			string(e.Confidence), e.ToolVersion, received)
		if err != nil {
			return 0, 0, 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			accepted++
			continue
		}
		n, err := applyRefresh(ctx, tx, e, refresh{update: true, fill: e.Model != event.UnknownModel})
		if err != nil {
			return 0, 0, 0, err
		}
		if n > 0 {
			updated++
		} else {
			duplicates++
		}
	}
	return accepted, duplicates, updated, nil
}

func dumpEvents(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.DB().Query(`SELECT ` + eventCols + ` FROM usage_events ORDER BY event_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		vals := make([]any, 17)
		ptrs := make([]any, 17)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprint(vals))
	}
	return out
}

// Random batches with repeats inside the batch, copies of stored events, other
// devices' ids, growing output and model placeholders: the batched insert must
// give the same counts and the same stored rows as the one-by-one reference.
func TestInsertEventsMatchesReference(t *testing.T) {
	ctx := context.Background()
	rng := rand.New(rand.NewPCG(1, 2))
	models := []string{event.UnknownModel, "claude-opus-5", "", "gpt-5.6-terra"}
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	for round := range 40 {
		batched, ref := openTest(t), openTest(t)
		var devices []string
		for i := range 2 {
			a, _, err := batched.CreateDevice(ctx, fmt.Sprint("d", i))
			if err != nil {
				t.Fatal(err)
			}
			// Same ids in both databases, so the dumps compare.
			if _, err := ref.DB().Exec(`INSERT INTO devices(id, name, token_hash, token_created_at) VALUES($1, $2, $3, 0)`,
				a.ID, a.Name, "h"+a.ID); err != nil {
				t.Fatal(err)
			}
			devices = append(devices, a.ID)
		}
		received := int64(1_000)
		for b := range 4 {
			n := 1 + rng.IntN(30)
			evs := make([]event.UsageEvent, n)
			for i := range evs {
				evs[i] = event.UsageEvent{
					EventID:   fmt.Sprintf("e%02d", rng.IntN(25)),
					DeviceID:  devices[rng.IntN(len(devices))],
					Source:    event.SourceClaudeCode,
					Timestamp: base.Add(time.Duration(rng.IntN(1000)) * time.Minute),
					Model:     models[rng.IntN(len(models))],
					Input:     int64(rng.IntN(100)),
					Output:    int64(rng.IntN(6)),
				}
			}
			var want [3]int
			err := ref.writeTx(ctx, func(tx *sql.Tx) error {
				var err error
				want[0], want[1], want[2], err = referenceInsert(ctx, tx, evs, received)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			var got [3]int
			err = batched.writeTx(ctx, func(tx *sql.Tx) error {
				var err error
				got[0], got[1], got[2], err = insertEventsTx(ctx, tx, batched.batch, evs, received)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("round %d batch %d: counts %v, reference %v\nbatch %+v", round, b, got, want, evs)
			}
			if g, w := dumpEvents(t, batched), dumpEvents(t, ref); !reflect.DeepEqual(g, w) {
				t.Fatalf("round %d batch %d: stored rows differ\n got %v\nwant %v", round, b, g, w)
			}
		}
	}
}

// More events than one statement takes go in several chunks.
func TestInsertEventsLargeBatch(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d, _, err := s.CreateDevice(ctx, "big")
	if err != nil {
		t.Fatal(err)
	}
	s.batch = batchSize{rows: 3, ids: 5, prepare: true} // several chunks without a huge batch
	evs := make([]event.UsageEvent, 20)
	for i := range evs {
		evs[i] = event.UsageEvent{EventID: fmt.Sprint("big-", i), DeviceID: d.ID, Source: event.SourceCodex,
			Timestamp: time.Unix(int64(i), 0), Model: "m", Output: 1}
	}
	if acc, dup, upd, err := s.InsertEvents(ctx, evs); err != nil || acc != len(evs) || dup != 0 || upd != 0 {
		t.Fatalf("first insert = %d/%d/%d, %v", acc, dup, upd, err)
	}
	for i := range evs {
		evs[i].Output = 2
	}
	if acc, dup, upd, err := s.InsertEvents(ctx, evs); err != nil || acc != 0 || dup != 0 || upd != len(evs) {
		t.Fatalf("refresh = %d/%d/%d, %v", acc, dup, upd, err)
	}
	if acc, dup, upd, err := s.InsertEvents(ctx, evs); err != nil || acc != 0 || dup != len(evs) || upd != 0 {
		t.Fatalf("re-send = %d/%d/%d, %v", acc, dup, upd, err)
	}
}

func TestCleanText(t *testing.T) {
	for in, want := range map[string]string{
		"plain":        "plain",
		"":             "",
		"한글":           "한글",
		"a\x00b":       "a�b",
		"bad\xffutf8":  "bad�utf8",
		"\x00\xfe\xff": "��",
	} {
		if got := CleanText(in); got != want {
			t.Errorf("CleanText(%q) = %q, want %q", in, got, want)
		}
		if IsCleanText(in) != (in == want) {
			t.Errorf("IsCleanText(%q) = %v", in, IsCleanText(in))
		}
	}
}

// Free text an agent sends is stored cleaned.
func TestInsertCleansFreeText(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d, _, err := s.CreateDevice(ctx, "n\x00ame")
	if err != nil {
		t.Fatal(err)
	}
	e := event.UsageEvent{EventID: "x", DeviceID: d.ID, Source: "claude\x00code", Timestamp: time.Unix(0, 0),
		Model: "m\x00", Project: "/p\xff", ToolVersion: "1\x00", Confidence: "exact\x00"}
	if _, _, _, err := s.InsertEvents(ctx, []event.UsageEvent{e}); err != nil {
		t.Fatal(err)
	}
	var src, model, project, tool, conf string
	if err := s.DB().QueryRow(`SELECT source, model, project, tool_version, confidence FROM usage_events`).
		Scan(&src, &model, &project, &tool, &conf); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{src, model, project, tool, conf} {
		if !IsCleanText(v) {
			t.Errorf("stored %q", v)
		}
	}
	if got, err := s.Device(ctx, d.ID); err != nil || got.Name != "n�ame" {
		t.Errorf("device name = %q, %v", got.Name, err)
	}
}

// Every query uses numbered parameters ($1, $2, ...), the one style both
// SQLite and PostgreSQL accept. A "?" left in a string literal of this package
// would bind silently wrong on SQLite when mixed with numbered ones.
func TestNoQuestionMarkPlaceholders(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			// The SQLite DSN carries a URL query string, not SQL.
			if strings.Contains(lit.Value, "?") && !strings.Contains(lit.Value, "_pragma=") {
				t.Errorf("%s: string literal with '?': %s", fset.Position(lit.Pos()), lit.Value)
			}
			return true
		})
	}
}

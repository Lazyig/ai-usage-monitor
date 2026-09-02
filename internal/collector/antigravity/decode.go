package antigravity

// Field-map specific decoding. Every path below is quoted from PLAN.md §9.2
// (gen_metadata.data) and §9.3 (steps.metadata); keep the two in sync.

import "time"

// step_type values seen in the wild (PLAN.md §9.3). Only informational here:
// we key off the presence of usage/model, not the type.
const (
	stepTypeModelGeneration = 15 // one or more gen_metadata rows
	stepTypeTitleSummary    = 23 // usage in metadata.9 only, no gen row, no model
)

// usage is the token-accounting struct. It appears twice with the same layout:
// gen_metadata.data at 1.4, steps.metadata at 9.
//
// Fields 1, 6, 7 and 8 of that struct are NOT tokens (model constant, tool
// count, bot id, session kv) and are deliberately never read.
type usage struct {
	Uncached    int64  // .2  prompt tokens that were not a cache hit
	Cached      int64  // .5  cache-hit prompt tokens (absent => 0)
	Output      int64  // .3  total output, thoughts included
	Thoughts    int64  // .9  reasoning tokens; absent on Claude rows
	Response    int64  // .10 candidate tokens
	RequestID   string // .11 dedupe key (Gemini: base64-ish, Claude: req_vrtx_…)
	HasThoughts bool
	HasOutput   bool
	HasResponse bool
	Present     bool // at least one token field was found
}

// readUsage decodes the usage struct rooted at m.
func readUsage(m message) (usage, error) {
	var u usage
	get := func(num int, dst *int64) (bool, error) {
		v, ok, err := m.int64At(num)
		if err != nil {
			return false, err
		}
		if ok {
			*dst = v
			u.Present = true
		}
		return ok, nil
	}
	if _, err := get(2, &u.Uncached); err != nil {
		return u, err
	}
	if _, err := get(5, &u.Cached); err != nil {
		return u, err
	}
	ok, err := get(3, &u.Output)
	if err != nil {
		return u, err
	}
	u.HasOutput = ok
	if ok, err = get(9, &u.Thoughts); err != nil {
		return u, err
	}
	u.HasThoughts = ok
	if ok, err = get(10, &u.Response); err != nil {
		return u, err
	}
	u.HasResponse = ok
	id, ok, err := m.str(11)
	if err != nil {
		return u, err
	}
	if ok {
		u.RequestID = id
		u.Present = true
	}
	return u, nil
}

// identityOK is self-check 1 of PLAN.md §9.7: total output must be the sum of
// thoughts and response, or equal the response when thoughts are not recorded
// (Claude via Vertex). Rows that carry no output at all are not checked.
func (u usage) identityOK() bool {
	if !u.HasOutput || !u.HasResponse {
		return true
	}
	if u.HasThoughts {
		return u.Output == u.Thoughts+u.Response
	}
	return u.Output == u.Response
}

// promptTotal is uncached + cached, the quantity that grows monotonically as a
// conversation goes on (self-check 2).
func (u usage) promptTotal() int64 { return u.Uncached + u.Cached }

// readTime decodes a {1: unix seconds, 2: nanos} timestamp message.
func readTime(m message) (time.Time, bool, error) {
	sec, ok, err := m.int64At(1)
	if err != nil || !ok {
		return time.Time{}, false, err
	}
	nanos, _, err := m.int64At(2)
	if err != nil {
		return time.Time{}, false, err
	}
	if sec <= 0 {
		return time.Time{}, false, nil
	}
	return time.Unix(sec, nanos).UTC(), true, nil
}

// genRow is one model call from gen_metadata.
type genRow struct {
	Idx          int64
	StepUUID     string // 4, joins steps.metadata.12
	Model        string // 1.19
	Usage        usage  // 1.4
	ReqTime      time.Time
	HasReqTime   bool   // 1.9.4
	TrajectoryID string // 1.20[trajectory_id] -> SessionID
}

// decodeGen reads a gen_metadata.data blob. It never touches 1.1, 1.2.*, 1.8.*
// or 1.16.*, which hold system prompts, conversation text and tool schemas.
func decodeGen(idx int64, blob []byte) (genRow, error) {
	g := genRow{Idx: idx}
	root := message(blob)

	uuid, _, err := root.str(4)
	if err != nil {
		return g, err
	}
	g.StepUUID = uuid

	body, ok, err := root.sub(1)
	if err != nil || !ok {
		return g, err
	}
	if g.Model, _, err = body.str(19); err != nil {
		return g, err
	}
	if us, ok, err := body.sub(4); err != nil {
		return g, err
	} else if ok {
		if g.Usage, err = readUsage(us); err != nil {
			return g, err
		}
	}
	// 1.9.4 request time; only some rows carry it, it is the timestamp
	// fallback when the step UUID join misses.
	if ts, ok, err := body.sub(9, 4); err != nil {
		return g, err
	} else if ok {
		if g.ReqTime, g.HasReqTime, err = readTime(ts); err != nil {
			return g, err
		}
	}
	// 1.20 is a repeated {1: key, 2: value} list. Only trajectory_id is
	// needed, and its value is a plain string, so field 2 is never descended
	// into as a message.
	err = body.eachSub(20, func(kv message) bool {
		k, ok, kerr := kv.str(1)
		if kerr != nil || !ok || k != "trajectory_id" {
			return kerr == nil
		}
		g.TrajectoryID, _, _ = kv.str(2)
		return false
	})
	if err != nil {
		return g, err
	}
	return g, nil
}

// stepRow is one row of steps (metadata column only; step_payload is never read).
type stepRow struct {
	Idx            int64
	StepType       int64
	UUID           string // 12
	Time           time.Time
	HasTime        bool   // 1
	Usage          usage  // 9, same layout as gen 1.4
	TrajectoryID   string // 20.1
	ConversationID string // 20.4
}

func decodeStep(idx, stepType int64, blob []byte) (stepRow, error) {
	s := stepRow{Idx: idx, StepType: stepType}
	root := message(blob)

	if ts, ok, err := root.sub(1); err != nil {
		return s, err
	} else if ok {
		var terr error
		if s.Time, s.HasTime, terr = readTime(ts); terr != nil {
			return s, terr
		}
	}
	var err error
	if s.UUID, _, err = root.str(12); err != nil {
		return s, err
	}
	if us, ok, err := root.sub(9); err != nil {
		return s, err
	} else if ok {
		if s.Usage, err = readUsage(us); err != nil {
			return s, err
		}
	}
	if meta, ok, err := root.sub(20); err != nil {
		return s, err
	} else if ok {
		s.TrajectoryID, _, _ = meta.str(1)
		s.ConversationID, _, _ = meta.str(4)
	}
	return s, nil
}

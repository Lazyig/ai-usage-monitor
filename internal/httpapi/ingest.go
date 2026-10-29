package httpapi

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ai-usage-monitor/internal/event"
	"ai-usage-monitor/internal/store"
)

type ingestRequest struct {
	Device store.DeviceMeta   `json:"device"`
	Events []event.UsageEvent `json:"events"`
}

type ingestResponse struct {
	Accepted   int    `json:"accepted"`
	Duplicates int    `json:"duplicates"`
	Updated    int    `json:"updated,omitempty"` // known events refreshed with a more complete copy
	Rejected   int    `json:"rejected"`
	DeviceID   string `json:"device_id"`        // the token's device; agents adopt it
	Resync     bool   `json:"resync,omitempty"` // an operator purged this device: rescan from zero
}

var (
	errTooManyEvents = errors.New("too many events")
	errBodyTooLarge  = errors.New("decompressed body too large")
)

// decodeIngest reads an ingest body one event at a time and stops at the
// first event past MaxBatchEvents, so an oversized batch is refused before it
// is held in memory as a whole.
func decodeIngest(r io.Reader) (ingestRequest, error) {
	var req ingestRequest
	dec := json.NewDecoder(r)
	if err := expectDelim(dec, '{'); err != nil {
		return req, err
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return req, err
		}
		switch key, _ := tok.(string); key {
		case "device":
			if err := dec.Decode(&req.Device); err != nil {
				return req, err
			}
		case "events":
			tok, err := dec.Token()
			if err != nil {
				return req, err
			}
			if tok == nil {
				continue // "events": null, an empty batch
			}
			if d, ok := tok.(json.Delim); !ok || d != '[' {
				return req, fmt.Errorf("events: want an array, got %v", tok)
			}
			for dec.More() {
				if len(req.Events) == MaxBatchEvents {
					return req, errTooManyEvents
				}
				var e event.UsageEvent
				if err := dec.Decode(&e); err != nil {
					return req, err
				}
				req.Events = append(req.Events, e)
			}
			if err := expectDelim(dec, ']'); err != nil {
				return req, err
			}
		default:
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return req, err
			}
		}
	}
	return req, expectDelim(dec, '}')
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != want {
		return fmt.Errorf("want %q, got %v", want, tok)
	}
	return nil
}

// capReader fails once more than left bytes are read, unlike io.LimitReader,
// which would end the stream quietly and turn a bomb into "invalid JSON".
type capReader struct {
	r    io.Reader
	left int64
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, errBodyTooLarge
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}

func (a *api) handleIngest(w http.ResponseWriter, r *http.Request) {
	dev, _ := deviceFrom(r.Context())

	// Agents gzip the batch. The decompressed stream is capped as well so a
	// small compressed body cannot expand without bound.
	var body io.Reader = r.Body
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid gzip body")
			return
		}
		defer zr.Close()
		// A full batch (5,000 events) is about 2 MiB of JSON; twice the raw
		// cap leaves room and stops a gzip bomb long before it costs memory.
		body = &capReader{r: zr, left: MaxBodyBytes * 2}
	}

	req, err := decodeIngest(body)
	if err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr), errors.Is(err, errBodyTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		case errors.Is(err, errTooManyEvents):
			writeError(w, http.StatusBadRequest, fmt.Sprintf("batch too large: at most %d events", MaxBatchEvents))
		default:
			writeError(w, http.StatusBadRequest, "invalid JSON body")
		}
		return
	}
	// The token is the identity. Whatever id the agent put in device.id is
	// informational: everything is stored under the token's device, and the
	// response tells the agent which id to use from now on.
	req.Device.ID = dev.ID
	req.Device = req.Device.Clipped()

	// Drop anything a collector should never have produced; the count comes
	// back so the agent can notice a broken decoder.
	good := make([]event.UsageEvent, 0, len(req.Events))
	rejected := 0
	for _, e := range req.Events {
		e.DeviceID = dev.ID
		// Ids must also be storable as they are: free text is cleaned on the
		// way in, but cleaning an id could merge two different events.
		if !e.Valid() || !store.IsCleanText(e.EventID) || !store.IsCleanText(e.SessionID) {
			rejected++
			continue
		}
		e.Timestamp = e.Timestamp.UTC()
		good = append(good, e)
	}

	accepted, duplicates, updated, err := a.store.InsertEvents(r.Context(), good)
	if err != nil {
		a.log.Error("insert events", "device", dev.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "could not store events")
		return
	}
	if err := a.store.TouchDevice(r.Context(), req.Device, true, time.Now()); err != nil {
		a.log.Error("touch device", "device", dev.ID, "err", err)
	}
	a.forgetDevice(dev.ID) // metadata changed; next lookup re-reads it

	// Consumed here, after the batch is stored, so exactly one answer carries
	// it. Heartbeats (empty batches) count: an idle device must hear about a
	// purge too, and it is the only request it makes.
	resync, err := a.store.ConsumeResync(r.Context(), dev.ID)
	if err != nil {
		// The batch is already stored; failing the request would only make the
		// agent resend it. The flag stays set and rides the next response.
		a.log.Error("consume resync", "device", dev.ID, "err", err)
	}

	writeJSON(w, http.StatusOK, ingestResponse{
		Accepted: accepted, Duplicates: duplicates, Updated: updated, Rejected: rejected, DeviceID: dev.ID, Resync: resync,
	})
}

// handleWhoAmI lets an agent learn the device id its token is bound to, so the
// ids it bakes into EventIDs match the server from the very first batch.
func (a *api) handleWhoAmI(w http.ResponseWriter, r *http.Request) {
	dev, _ := deviceFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"device_id": dev.ID, "name": dev.Name})
}

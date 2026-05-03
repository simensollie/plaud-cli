package sync

import (
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/simensollie/plaud-cli/internal/api"
)

// EventSchemaVersion is locked at 1 from v0.3.0 GA. New event types and
// new fields inside details are allowed; removing or retyping anything
// requires a major version bump (F-07).
const EventSchemaVersion = 1

// envelope is the canonical NDJSON shape emitted by EventEmitter.
type envelope struct {
	SchemaVersion int            `json:"schema_version"`
	Event         string         `json:"event"`
	Ts            time.Time      `json:"ts"`
	ID            string         `json:"id,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
}

// EventEmitter writes one JSON object per call as a single line to Out.
// Thread-safe via an internal mutex; Now is injectable for tests.
type EventEmitter struct {
	Out io.Writer
	Now func() time.Time

	mu sync.Mutex
}

// Emit writes one event line. id may be empty (the JSON omits it). The
// details map is scrubbed via api.RedactString on the "error" field —
// surface-level F-13 backstop.
func (e *EventEmitter) Emit(event string, id string, details map[string]any) {
	if e == nil || e.Out == nil {
		return
	}
	now := e.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if details != nil {
		if msg, ok := details["error"].(string); ok && msg != "" {
			details["error"] = api.RedactString(msg)
		}
	}
	env := envelope{
		SchemaVersion: EventSchemaVersion,
		Event:         event,
		Ts:            now().UTC(),
		ID:            id,
		Details:       details,
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, _ = e.Out.Write(raw)
	_, _ = e.Out.Write([]byte("\n"))
}

// EventNames are the stable strings for the NDJSON `event` field. F-07.
const (
	EventNameDiscovered EventName = "discovered"
	EventNameSkipped    EventName = "skipped"
	EventNameFetched    EventName = "fetched"
	EventNameVerified   EventName = "verified"
	EventNameFailed     EventName = "failed"
	EventNamePruned     EventName = "pruned"
	EventNameWouldFetch EventName = "would-fetch"
	EventNameWouldSkip  EventName = "would-skip"
	EventNameWouldPrune EventName = "would-prune"
	EventNameDone       EventName = "done"
)

// EventName is the typed wrapper around the on-the-wire event string. The
// string itself is the contract; the type is just a convenience.
type EventName string

package events

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Recorder is an opt-in developer/support diagnostic sink. When wired to the broker
// it mirrors every broadcast event to a newline-delimited JSON file per call, so a
// self-hoster can attach a call's full timeline (status, quality, setup marks, relay,
// mute, end) to a support request. It records the same structured events the SSE
// stream carries: call metadata, never raw keys, secrets, or media.
//
// Every method is nil-safe so callers can hold a nil *Recorder when diagnostics are
// off and emit unconditionally at zero cost. Offer never blocks the caller: a full
// buffer drops the event (diagnostics must never stall a live call).
type Recorder struct {
	log  *slog.Logger
	dir  string
	ch   chan map[string]any
	done chan struct{}

	mu      sync.Mutex
	files   map[string]*os.File
	dropOne sync.Once
}

const diagBuffer = 256

// NewRecorder creates dir (and parents) and starts the drain goroutine. It returns
// an error only if dir cannot be created.
func NewRecorder(dir string, log *slog.Logger) (*Recorder, error) {
	if log == nil {
		log = slog.Default()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("diag: create dir %q: %w", dir, err)
	}
	r := &Recorder{
		log:   log,
		dir:   dir,
		ch:    make(chan map[string]any, diagBuffer),
		done:  make(chan struct{}),
		files: make(map[string]*os.File),
	}
	go r.drain()
	return r, nil
}

// Offer hands one broadcast event to the recorder without blocking. A nil recorder
// or a full buffer is a no-op (the latter increments a dropped-events warning once).
func (r *Recorder) Offer(ev map[string]any) {
	if r == nil {
		return
	}
	select {
	case r.ch <- ev:
	default:
		r.dropOne.Do(func() {
			r.log.Warn("diagnostics buffer full; dropping call diagnostic events", "dir", r.dir)
		})
	}
}

func (r *Recorder) drain() {
	defer close(r.done)
	for ev := range r.ch {
		r.write(ev)
	}
}

// write appends one event as a JSON line to its per-call (or session) file, stamping
// the recorder's own ts_ms. Open and write failures are swallowed so diagnostics
// never break a live call.
func (r *Recorder) write(ev map[string]any) {
	rec := make(map[string]any, len(ev)+1)
	maps.Copy(rec, ev)
	rec["ts_ms"] = time.Now().UnixMilli()

	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	f := r.fileFor(ev)
	if f == nil {
		return
	}
	_, _ = f.Write(append(data, '\n'))
}

// fileFor returns the open file for the event's call (call-<id>.jsonl) or the shared
// session.jsonl when the event carries no call id. Files are opened lazily.
func (r *Recorder) fileFor(ev map[string]any) *os.File {
	name := "session"
	if id, ok := ev["id"].(string); ok && id != "" {
		name = "call-" + id
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if f := r.files[name]; f != nil {
		return f
	}
	path := filepath.Join(r.dir, name+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	r.files[name] = f
	return f
}

// Close stops the drain goroutine, flushing queued events, and closes every open
// file. It is a no-op on a nil recorder and returns the first close error.
func (r *Recorder) Close() error {
	if r == nil {
		return nil
	}
	close(r.ch)
	<-r.done

	r.mu.Lock()
	defer r.mu.Unlock()
	var firstErr error
	for name, f := range r.files {
		if err := f.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(r.files, name)
	}
	return firstErr
}

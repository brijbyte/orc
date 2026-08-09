// Package web serves a session over HTTP: an SSE event stream plus input,
// mirroring agent.IO events to every connected browser tab.
package web

import (
	"encoding/json"
	"sync"
)

// Event is one broadcast item; ID is 1-based and doubles as the SSE id.
type Event struct {
	ID   int64           `json:"id"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// hub is the append-only event log plus subscriber wakeup.
type hub struct {
	mu     sync.Mutex
	cond   *sync.Cond
	events []Event
	closed bool
	busy   bool
	status string
}

func newHub() *hub {
	h := &hub{}
	h.cond = sync.NewCond(&h.mu)
	return h
}

func (h *hub) emit(typ string, data any) {
	raw, _ := json.Marshal(data)
	h.mu.Lock()
	h.events = append(h.events, Event{ID: int64(len(h.events) + 1), Type: typ, Data: raw})
	h.mu.Unlock()
	h.cond.Broadcast()
}

func (h *hub) close() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	h.cond.Broadcast()
}

// waitAfter blocks until events beyond id exist (or the hub closes) and
// returns a copy of them. done=true means no more events will come.
func (h *hub) waitAfter(id int64) (evs []Event, done bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for int64(len(h.events)) <= id && !h.closed {
		h.cond.Wait()
	}
	if int64(len(h.events)) > id {
		evs = append(evs, h.events[id:]...)
	}
	return evs, h.closed
}

func (h *hub) snapshot() (evs []Event, busy bool, status string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Event{}, h.events...), h.busy, h.status
}

// page returns complete display blocks before the given event ID. A zero
// before value selects the newest page.
func (h *hub) page(before int64, limit int) (evs []Event, cursor, last int64, more, busy bool, status string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	end := len(h.events)
	if before > 0 && before-1 < int64(end) {
		end = int(before - 1)
	}
	starts := blockStarts(h.events[:end])
	start := 0
	if len(starts) > limit {
		start = starts[len(starts)-limit]
		more = true
	}
	evs = append(evs, h.events[start:end]...)
	if len(evs) > 0 {
		cursor = evs[0].ID
	}
	return evs, cursor, int64(len(h.events)), more, h.busy, h.status
}

func blockStarts(events []Event) []int {
	var starts []int
	last := ""
	assistantOpen := false
	for i, ev := range events {
		switch ev.Type {
		case "user", "pending", "tool", "notice":
			starts = append(starts, i)
			last, assistantOpen = ev.Type, false
		case "delta":
			if last != "assistant" || !assistantOpen {
				starts = append(starts, i)
			}
			last, assistantOpen = "assistant", true
		case "think":
			if last != "think" {
				starts = append(starts, i)
			}
			last, assistantOpen = "think", false
		case "turn_end":
			if last == "assistant" {
				assistantOpen = false
			}
		}
	}
	return starts
}

func (h *hub) setBusy(b bool) {
	h.mu.Lock()
	h.busy = b
	h.mu.Unlock()
	h.emit("busy", map[string]bool{"busy": b})
}

func (h *hub) setStatus(s string) {
	h.mu.Lock()
	changed := h.status != s
	h.status = s
	h.mu.Unlock()
	if changed {
		h.emit("status", map[string]string{"text": s})
	}
}

package web

import "testing"

func TestHubPageKeepsBlockEventsTogether(t *testing.T) {
	h := newHub()
	h.emit("user", nil)
	h.emit("turn_begin", nil)
	h.emit("delta", nil)
	h.emit("delta", nil)
	h.emit("turn_end", nil)
	h.emit("tool", nil)
	h.emit("think", nil)
	h.emit("think", nil)

	events, cursor, last, more, _, _ := h.page(0, 2)
	if cursor != 6 || last != 8 || !more {
		t.Fatalf("newest page: cursor=%d last=%d more=%v", cursor, last, more)
	}
	if len(events) != 3 || events[0].Type != "tool" || events[2].Type != "think" {
		t.Fatalf("newest page events: %#v", events)
	}

	events, cursor, _, more, _, _ = h.page(cursor, 2)
	if cursor != 1 || more {
		t.Fatalf("older page: cursor=%d more=%v", cursor, more)
	}
	if len(events) != 5 || events[0].Type != "user" || events[4].Type != "turn_end" {
		t.Fatalf("older page events: %#v", events)
	}
}

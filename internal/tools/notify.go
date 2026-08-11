package tools

import (
	"context"

	"github.com/brijbyte/orc/internal/notify"
)

var urgencies = map[string]bool{"info": true, "warn": true, "urgent": true}

// toolNotify reaches the user through their configured channels. Which
// channels those are — and why one failed — stays out of the tool output:
// the model picks the message, the user picks the transport.
func toolNotify(ctx context.Context, a args) string {
	title, body := a.str("title"), a.str("body")
	if title == "" && body == "" {
		return "error: title or body is required"
	}
	urgency := a.str("urgency")
	if !urgencies[urgency] {
		urgency = "info"
	}
	if !notify.Configured() {
		return "not sent: the user has set up no way to be reached"
	}
	sent, err := notify.SendAway(ctx, notify.Message{
		Title: title, Body: body, Urgency: urgency,
	})
	switch {
	case !sent:
		return "not sent: the user is watching the session"
	case err != nil:
		return "error: could not reach the user"
	}
	return "sent"
}

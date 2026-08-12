package tools

import (
	"context"

	"github.com/brijbyte/orc/internal/notify"
)

var urgencies = map[string]bool{"info": true, "warn": true, "urgent": true}

// toolNotify reaches the user through their configured channels. Which
// channels those are — and why one failed — stays out of the tool output:
// the model picks the message, the user picks the transport. Routines push
// even while a UI is attached; interactive sessions only push while away.
func toolNotify(ctx context.Context, a args, routine bool) string {
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
	message := notify.Message{
		Title: title, Body: body, URL: a.str("url"), Urgency: urgency,
	}
	if routine {
		if err := notify.Send(ctx, message); err != nil {
			return "error: could not reach the user"
		}
		return "sent"
	}
	sent, err := notify.SendAway(ctx, message)
	switch {
	case !sent:
		return "not sent: the user is watching the session"
	case err != nil:
		return "error: could not reach the user"
	}
	return "sent"
}

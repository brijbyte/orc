// Package notify fans out agent notifications to user-configured channels.
package notify

import (
	"context"
	"errors"
	"fmt"

	"github.com/brijbyte/orc/internal/config"
)

// Message is one notification. Urgency is "info", "warn", or "urgent".
type Message struct {
	Title   string
	Body    string
	URL     string // deep link back to the session; may be empty
	Urgency string
}

// Field describes one settings input a channel type needs.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Optional    bool   `json:"optional,omitempty"`
}

// Type is a channel provider: how to configure it and how to send.
type Type struct {
	ID     string                                                                 `json:"id"`
	Label  string                                                                 `json:"label"`
	Fields []Field                                                                `json:"fields"`
	Send   func(ctx context.Context, settings map[string]string, m Message) error `json:"-"`
}

var types []Type

func register(t Type) { types = append(types, t) }

// Types lists the available channel providers.
func Types() []Type { return types }

func typeByID(id string) *Type {
	for i := range types {
		if types[i].ID == id {
			return &types[i]
		}
	}
	return nil
}

// Validate checks a channel's type and required fields.
func Validate(ch config.NotifyChannel) error {
	t := typeByID(ch.Type)
	if t == nil {
		return fmt.Errorf("unknown channel type %q", ch.Type)
	}
	for _, f := range t.Fields {
		if !f.Optional && ch.Settings[f.Key] == "" {
			return fmt.Errorf("%s: %s is required", t.Label, f.Label)
		}
	}
	return nil
}

// SendTo delivers a message on one channel.
func SendTo(ctx context.Context, ch config.NotifyChannel, m Message) error {
	if err := Validate(ch); err != nil {
		return err
	}
	return typeByID(ch.Type).Send(ctx, ch.Settings, m)
}

// Send fans a message out to every enabled channel; errors are joined.
func Send(ctx context.Context, m Message) error {
	var errs []error
	for _, ch := range config.LoadSettings().Notify {
		if !ch.Enabled {
			continue
		}
		if err := SendTo(ctx, ch, m); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ch.Name, err))
		}
	}
	return errors.Join(errs...)
}

// Package provider defines model backends. History items use the canonical
// format (Responses-API input items); a provider that speaks a different wire
// format translates at request-build/parse time only.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/brijbyte/orc/internal/config"
)

// ErrInterrupted reports a turn canceled by the user.
var ErrInterrupted = errors.New("interrupted")

type Callbacks struct {
	OnTextDelta     func(string)
	OnThinkingDelta func(string)
	// Complete output item (message / function_call / reasoning).
	OnItemDone func(json.RawMessage)
	// Tokens now occupying the context (usage total after a request).
	OnUsage func(int64)
}

type Model struct {
	Slug          string
	Description   string
	ContextWindow int64
}

type Provider interface {
	Name() string
	DefaultModel() string
	// Turn runs one model request over history and tools, streaming via cb.
	Turn(ctx context.Context, history []json.RawMessage, tools json.RawMessage,
		cfg *config.Config, cb *Callbacks) error
	// AuthStatus prints auth status for --auth; nil if usable.
	AuthStatus() error
	// Login runs interactive login for --login; nil when unsupported.
	Login() error
	// Models lists selectable models; nil when unavailable.
	Models() []Model
}

var registry []Provider

func Register(p Provider) { registry = append(registry, p) }

// Get looks up by name; "" returns the default provider. nil if unknown.
func Get(name string) Provider {
	if name == "" {
		return registry[0]
	}
	for _, p := range registry {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

func List() {
	for _, p := range registry {
		fmt.Printf("  %s (default model %s)\n", p.Name(), p.DefaultModel())
	}
}

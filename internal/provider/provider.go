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
	// Progress diagnostics (retries, token refresh) as whole lines.
	OnNotice func(string)
}

type Model struct {
	Slug          string
	Name          string // human label; empty falls back to Slug
	Description   string
	Efforts       []string // selectable reasoning efforts; empty = default set
	ContextWindow int64
}

// WebAuthState is the provider sign-in state safe to expose to the web UI.
type WebAuthState struct {
	Authenticated bool   `json:"authenticated"`
	ExpiresAt     string `json:"expires_at,omitempty"`
}

// WebAuthenticator is optionally implemented by providers with a browser flow
// that can be completed by pasting its callback URL or authorization code.
type WebAuthenticator interface {
	WebAuthStatus() WebAuthState
	BeginWebLogin() (string, error)
	CompleteWebLogin(context.Context, string) error
}

type Provider interface {
	Name() string
	DefaultModel() string
	// Turn runs one model request over history and tools, streaming via cb.
	Turn(ctx context.Context, history []json.RawMessage, tools json.RawMessage,
		cfg *config.Config, cb *Callbacks) error
	// AuthStatus prints auth status for --auth; nil if usable.
	AuthStatus() error
	// Authenticated quietly reports whether usable credentials exist.
	Authenticated() bool
	// Login runs the OAuth flow, reporting progress via notify; ctx cancels
	// the wait for the browser callback.
	Login(ctx context.Context, notify func(string)) error
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

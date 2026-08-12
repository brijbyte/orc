package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brijbyte/orc/internal/config"
	"github.com/brijbyte/orc/internal/provider"
	"github.com/brijbyte/orc/internal/session"
)

type recoveryProvider struct{}

func (recoveryProvider) Name() string                              { return "recovery" }
func (recoveryProvider) DefaultModel() string                      { return "test" }
func (recoveryProvider) AuthStatus() error                         { return nil }
func (recoveryProvider) Authenticated() bool                       { return true }
func (recoveryProvider) Login(context.Context, func(string)) error { return nil }
func (recoveryProvider) Models() []provider.Model                  { return nil }
func (recoveryProvider) Turn(context.Context, []json.RawMessage, json.RawMessage, *config.Config, *provider.Callbacks) error {
	return nil
}

type recoveryIO struct{}

func (recoveryIO) TurnBegin() error                        { return nil }
func (recoveryIO) TextDelta(string)                        {}
func (recoveryIO) ThinkingDelta(string)                    {}
func (recoveryIO) TurnEnd()                                {}
func (recoveryIO) ToolCall(string, string)                 {}
func (recoveryIO) UserLine(string)                         {}
func (recoveryIO) Replay([]json.RawMessage)                {}
func (recoveryIO) Usage(int64)                             {}
func (recoveryIO) Notice(string)                           {}
func (recoveryIO) QueueDrain()                             {}
func (recoveryIO) QueuePeek() (string, bool)               { return "", false }
func (recoveryIO) QueueTake() (string, []Attachment, bool) { return "", nil, false }

func TestInterruptedToolCallsGetOutputsWithoutExecution(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := &config.Config{SessionID: "12345678-1234-1234-1234-123456789abc", Cwd: t.TempDir()}
	sess, err := session.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	call := json.RawMessage(`{"type":"function_call","name":"bash","call_id":"call-1","arguments":"{\"cmd\":\"touch bad\"}"}`)
	sess.Append(call)
	sess.ToolsBegin()
	sess.Close()

	resumed, history, err := session.Resume(cfg.SessionID, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	ag := New(cfg, recoveryProvider{}, resumed, history, recoveryIO{})
	if len(ag.History) != 2 {
		t.Fatalf("history = %d items, want call plus synthetic output", len(ag.History))
	}
	var out item
	if json.Unmarshal(ag.History[1], &out) != nil || out.Type != "function_call_output" || out.CallID != "call-1" {
		t.Fatalf("synthetic output = %s", ag.History[1])
	}
}

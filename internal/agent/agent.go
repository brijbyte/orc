// Package agent owns the turn and tool-call loop.
package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/brijbyte/orc/internal/config"
	"github.com/brijbyte/orc/internal/instructions"
	"github.com/brijbyte/orc/internal/provider"
	"github.com/brijbyte/orc/internal/session"
	"github.com/brijbyte/orc/internal/tools"
)

// IO is the interface the UI supplies for agent events and queued input.
type IO interface {
	TurnBegin() error
	TextDelta(text string)
	ThinkingDelta(text string)
	TurnEnd()
	ToolCall(name, argsJSON string)
	UserLine(line string)
	Replay(history []json.RawMessage)
	Usage(tokens int64)
	Notice(line string)
	QueueDrain()
	QueuePeek() (string, bool)
	QueueTake() (string, bool)
}

type Agent struct {
	History []json.RawMessage
	Cfg     *config.Config
	Sess    *session.Session
	Prov    provider.Provider
	IO      IO

	tools json.RawMessage
}

// item is the subset of a history item the agent inspects.
type item struct {
	Type      string `json:"type"`
	Role      string `json:"role"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	CallID    string `json:"call_id"`
}

func New(cfg *config.Config, prov provider.Provider, sess *session.Session,
	resumed []json.RawMessage, io IO) *Agent {
	return &Agent{
		History: resumed,
		Cfg:     cfg,
		Sess:    sess,
		Prov:    prov,
		IO:      io,
		tools:   json.RawMessage(tools.SchemaJSON),
	}
}

func userMessage(text string) json.RawMessage {
	msg, _ := json.Marshal(map[string]any{
		"type": "message",
		"role": "user",
		"content": []map[string]string{
			{"type": "input_text", "text": text},
		},
	})
	return msg
}

func callOutput(callID, output string) json.RawMessage {
	out, _ := json.Marshal(map[string]string{
		"type":    "function_call_output",
		"call_id": callID,
		"output":  output,
	})
	return out
}

func (ag *Agent) commit(raw json.RawMessage) {
	ag.Sess.Append(raw)
	ag.History = append(ag.History, raw)
}

func parseItem(raw json.RawMessage) item {
	var it item
	json.Unmarshal(raw, &it)
	return it
}

func (ag *Agent) runCall(ctx context.Context, call item) {
	if call.Name == "" || call.CallID == "" {
		return
	}
	ag.IO.ToolCall(call.Name, call.Arguments)
	output := tools.Run(ctx, call.Name, call.Arguments)
	ag.commit(callOutput(call.CallID, output))
}

// isControlLine reports a slash command or exit word: runs after the turn,
// never sent as steering. Slash lines are never model input.
func isControlLine(s string) bool {
	return s == "exit" || s == "quit" || strings.HasPrefix(s, "/")
}

// steer injects lines queued during the turn as user messages, so the model
// sees them at the next request (pi-style steering between tool rounds).
func (ag *Agent) steer() {
	ag.IO.QueueDrain()
	for {
		peek, ok := ag.IO.QueuePeek()
		if !ok || isControlLine(peek) {
			return
		}
		line, _ := ag.IO.QueueTake()
		ag.IO.UserLine(line)
		ag.commit(userMessage(line))
	}
}

func (ag *Agent) Replay() { ag.IO.Replay(ag.History) }

// Turn runs one user turn to completion (including tool rounds).
func (ag *Agent) Turn(ctx context.Context, userText string) error {
	if ag.Cfg.Instructions == "" {
		ag.Cfg.Instructions = instructions.Build()
	}
	ag.commit(userMessage(userText))

	for {
		var pending []json.RawMessage
		if err := ag.IO.TurnBegin(); err != nil {
			return err
		}
		cb := &provider.Callbacks{
			OnTextDelta:     ag.IO.TextDelta,
			OnThinkingDelta: ag.IO.ThinkingDelta,
			OnItemDone:      func(it json.RawMessage) { pending = append(pending, it) },
			OnUsage: func(tokens int64) {
				ag.Sess.SetCtx(tokens)
				ag.IO.Usage(tokens)
			},
			OnNotice: ag.IO.Notice,
		}
		err := ag.Prov.Turn(ctx, ag.History, ag.tools, ag.Cfg, cb)
		ag.IO.TurnEnd()
		if err != nil {
			return err
		}

		// Commit items in order; collect the function calls.
		var calls []item
		for _, raw := range pending {
			ag.commit(raw)
			if it := parseItem(raw); it.Type == "function_call" {
				calls = append(calls, it)
			}
		}
		if len(calls) == 0 {
			return nil
		}

		interrupted := false
		for _, call := range calls {
			if ctx.Err() != nil {
				interrupted = true
			}
			if interrupted {
				// Committed calls must still get outputs or the next request 400s.
				ag.commit(callOutput(call.CallID, "[interrupted by user]"))
			} else {
				ag.runCall(ctx, call)
			}
		}
		if interrupted {
			return provider.ErrInterrupted
		}
		ag.steer()
	}
}

package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/brijbyte/orc/internal/commands"
	"github.com/brijbyte/orc/internal/ui"
)

// IO implements agent.IO and commands.UI over the web hub, and gives the
// driver the same queue/busy/cancel surface as the TUI.
type IO struct {
	hub   *hub
	q     *queue
	token string
	cmds  *commands.Commands

	mu     sync.Mutex
	cancel context.CancelFunc
}

func NewIO() *IO {
	buf := make([]byte, 16)
	rand.Read(buf)
	return &IO{hub: newHub(), q: newQueue(), token: hex.EncodeToString(buf)}
}

func (w *IO) Token() string { return w.token }

func (w *IO) SetCommands(c *commands.Commands) { w.cmds = c }

func text(s string) map[string]string { return map[string]string{"text": s} }

// --- agent.IO ---

func (w *IO) TurnBegin() error {
	w.hub.emit("turn_begin", struct{}{})
	return nil
}

func (w *IO) TextDelta(s string)     { w.hub.emit("delta", text(s)) }
func (w *IO) ThinkingDelta(s string) { w.hub.emit("think", text(s)) }
func (w *IO) TurnEnd()               { w.hub.emit("turn_end", struct{}{}) }

func (w *IO) ToolCall(name, argsJSON string) {
	w.hub.emit("tool", map[string]string{
		"name": name,
		"desc": ui.ToolDesc(name, argsJSON),
		"diff": ui.EditDiff(name, argsJSON, false),
	})
}

func (w *IO) UserLine(line string) { w.hub.emit("user", text(line)) }
func (w *IO) Notice(line string)   { w.hub.emit("notice", text(line)) }

func (w *IO) Usage(tokens int64) {
	if w.cmds != nil {
		w.cmds.CtxUsed(tokens)
	}
}

// Replay seeds the event log from resumed history so a fresh tab renders
// the whole session.
func (w *IO) Replay(history []json.RawMessage) {
	for _, raw := range history {
		var probe struct {
			Type      string `json:"type"`
			Role      string `json:"role"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &probe) != nil {
			continue
		}
		switch probe.Type {
		case "function_call":
			w.ToolCall(probe.Name, probe.Arguments)
		case "message":
			var sb strings.Builder
			for _, part := range probe.Content {
				sb.WriteString(part.Text)
			}
			if sb.Len() == 0 {
				continue
			}
			if probe.Role == "user" {
				w.UserLine(sb.String())
			} else {
				w.hub.emit("turn_begin", struct{}{})
				w.hub.emit("delta", text(sb.String()))
				w.hub.emit("turn_end", struct{}{})
			}
		}
	}
}

func (w *IO) QueueDrain()               {}
func (w *IO) QueuePeek() (string, bool) { return w.q.peek() }
func (w *IO) QueueTake() (string, bool) {
	it, ok := w.q.take()
	return it.line, ok
}

// --- commands.UI ---

func (w *IO) Printf(format string, a ...any) { w.Notice(fmt.Sprintf(format, a...)) }
func (w *IO) SetStatus(s string)             { w.hub.setStatus(s) }

// --- driver support (same surface as the TUI) ---

func (w *IO) WaitTake() (line string, queued, ok bool) {
	it, ok := w.q.waitTake()
	return it.line, it.queued, ok
}

func (w *IO) SetBusy(b bool) { w.hub.setBusy(b) }

func (w *IO) SetCancel(cancel context.CancelFunc) {
	w.mu.Lock()
	w.cancel = cancel
	w.mu.Unlock()
}

func (w *IO) Interrupt() {
	w.mu.Lock()
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Unlock()
}

// EchoQueued reprints a queued line as it starts running.
func (w *IO) EchoQueued(line string) { w.UserLine(line) }

// Close ends the input queue and the SSE streams.
func (w *IO) Close() {
	w.q.setEOF()
	w.hub.close()
}

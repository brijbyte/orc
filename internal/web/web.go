package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/brijbyte/orc/internal/agent"
	"github.com/brijbyte/orc/internal/commands"
	"github.com/brijbyte/orc/internal/ui"
)

// IO implements agent.IO and commands.UI over the web hub, and gives the
// driver the same queue/busy/cancel surface as the TUI.
type IO struct {
	hub  *hub
	q    *queue
	cmds *commands.Commands

	mu     sync.Mutex
	cancel context.CancelFunc
}

func NewIO() *IO {
	return &IO{hub: newHub(), q: newQueue()}
}

func (w *IO) SetCommands(c *commands.Commands) { w.cmds = c }

// Busy reports whether a turn is running (for the session list).
func (w *IO) Busy() bool {
	_, busy, _ := w.hub.snapshot()
	return busy
}

func text(s string) map[string]string { return map[string]string{"text": s} }

// --- agent.IO ---

func (w *IO) TurnBegin() error {
	w.hub.emit("turn_begin", struct{}{})
	return nil
}

func (w *IO) TextDelta(s string)     { w.hub.emit("delta", text(s)) }
func (w *IO) ThinkingDelta(s string) { w.hub.emit("think", text(s)) }
func (w *IO) TurnEnd()               { w.hub.emit("turn_end", struct{}{}) }

// toolCopyText is the raw source a copy button should yield: the command,
// the written content, or an edit's replacement.
func toolCopyText(name, argsJSON string) string {
	var a struct{ Cmd, Content, New string }
	if json.Unmarshal([]byte(argsJSON), &a) != nil {
		return ""
	}
	switch name {
	case "bash":
		return a.Cmd
	case "write":
		return a.Content
	case "edit":
		return a.New
	}
	return ""
}

func (w *IO) ToolCall(name, argsJSON string) {
	// full preview: the frontend truncates and expands client-side; write
	// content is pre-highlighted to HTML lines so the browser needs no lexer
	_, full := ui.ToolPreview(name, argsJSON, false, "")
	desc := ui.ToolDesc(name, argsJSON)
	if name == "bash" {
		// bash previews are the raw command, no line-number gutter; the
		// one-liner stays empty so the command is not shown twice
		var a struct{ Cmd string }
		full = ""
		if json.Unmarshal([]byte(argsJSON), &a) == nil {
			cmd := strings.TrimRight(a.Cmd, "\n")
			if strings.Contains(cmd, "\n") || len([]rune(cmd)) > ui.DescMax {
				full, desc = cmd, ""
			}
		}
	}
	data := map[string]any{
		"name":    name,
		"desc":    desc,
		"preview": full,
	}
	var args struct{ Path string }
	if json.Unmarshal([]byte(argsJSON), &args) == nil && args.Path != "" {
		data["path"] = args.Path
	}
	if hl := ui.PreviewHTML(name, argsJSON); hl != nil {
		data["html"] = hl
	}
	if c := toolCopyText(name, argsJSON); c != "" {
		data["copy"] = c
	}
	w.hub.emit("tool", data)
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
				Type string `json:"type"`
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
				if part.Type == "input_image" {
					sb.WriteString("\n📎 image")
					continue
				}
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
func (w *IO) QueueTake() (string, []agent.Attachment, bool) {
	it, ok := w.q.take()
	return it.line, it.atts, ok
}

// --- commands.UI ---

func (w *IO) Printf(format string, a ...any) { w.Notice(fmt.Sprintf(format, a...)) }
func (w *IO) SetStatus(s string)             { w.hub.setStatus(s) }

// --- driver support (same surface as the TUI) ---

func (w *IO) WaitTake() (line string, atts []agent.Attachment, queued, ok bool) {
	it, ok := w.q.waitTake()
	return it.line, it.atts, it.queued, ok
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

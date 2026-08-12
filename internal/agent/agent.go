// Package agent owns the turn and tool-call loop.
package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brijbyte/orc/internal/config"
	"github.com/brijbyte/orc/internal/instructions"
	"github.com/brijbyte/orc/internal/notify"
	"github.com/brijbyte/orc/internal/provider"
	"github.com/brijbyte/orc/internal/session"
	"github.com/brijbyte/orc/internal/tools"
	"github.com/google/uuid"
)

// Attachment is a user-supplied file sent with a turn.
type Attachment struct {
	Name string
	Mime string
	Data []byte
	Path string // server path; content is not loaded
}

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
	QueueTake() (string, []Attachment, bool)
}

type Agent struct {
	History []json.RawMessage
	Cfg     *config.Config
	Sess    *session.Session
	Prov    provider.Provider
	IO      IO

	tools       json.RawMessage
	interrupted bool
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
	ag := &Agent{
		History: resumed,
		Cfg:     cfg,
		Sess:    sess,
		Prov:    prov,
		IO:      io,
		tools:   tools.Schema(cfg.Routine != ""),
	}
	if sess.InterruptedTools {
		ag.completeInterruptedCalls()
	}
	ag.interrupted = sess.Interrupted
	return ag
}

func (ag *Agent) Interrupted() bool { return ag.interrupted }

// completeInterruptedCalls makes a crashed tool round valid Responses input
// without executing tools again. The user may then explicitly retry the model.
func (ag *Agent) completeInterruptedCalls() {
	outputs := map[string]bool{}
	for _, raw := range ag.History {
		it := parseItem(raw)
		if it.Type == "function_call_output" {
			outputs[it.CallID] = true
		}
	}
	for _, raw := range append([]json.RawMessage{}, ag.History...) {
		it := parseItem(raw)
		if it.Type == "function_call" && !outputs[it.CallID] {
			ag.commit(callOutput(it.CallID, "[interrupted by service restart; not executed]"))
		}
	}
}

// userMessage builds a user item: the text plus one content part per
// attachment — images inline as data-URL input_image, text files inline as
// input_text, other binaries as a note.
func userMessage(text string, atts []Attachment) json.RawMessage {
	var content []map[string]string
	if text != "" || len(atts) == 0 {
		content = append(content, map[string]string{"type": "input_text", "text": text})
	}
	for _, a := range atts {
		switch {
		case a.Path != "":
			content = append(content, map[string]string{
				"type": "input_text",
				"text": fmt.Sprintf("[attached server path: %s]", a.Path),
			})
		case strings.HasPrefix(a.Mime, "image/"):
			content = append(content, map[string]string{
				"type":      "input_image",
				"image_url": "data:" + a.Mime + ";base64," + base64.StdEncoding.EncodeToString(a.Data),
			})
		case utf8.Valid(a.Data):
			content = append(content, map[string]string{
				"type": "input_text",
				"text": fmt.Sprintf("[attached file: %s]\n%s", a.Name, a.Data),
			})
		default:
			content = append(content, map[string]string{
				"type": "input_text",
				"text": fmt.Sprintf("[attached binary file %s: %d bytes, content omitted]", a.Name, len(a.Data)),
			})
		}
	}
	msg, _ := json.Marshal(map[string]any{
		"type": "message", "role": "user", "content": content,
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

func (ag *Agent) runCall(ctx context.Context, call item) bool {
	if call.Name == "" || call.CallID == "" {
		return false
	}
	ag.IO.ToolCall(call.Name, call.Arguments)
	var routine *tools.RoutineCallbacks
	if ag.Cfg.Routine != "" {
		routine = &tools.RoutineCallbacks{
			Sleep: func(wake time.Time, _ string) { ag.Sess.SetWake(wake.Format(time.RFC3339)) },
			Stop: func(string) {
				ag.Sess.StopRoutine()
				ag.Cfg.Routine = ""
			},
		}
	}
	output, endTurn := tools.RunWithRoutine(ctx, ag.Cfg.Cwd, call.Name, call.Arguments, routine)
	ag.commit(callOutput(call.CallID, output))
	return endTurn
}

// Echo is the display form of a user line with its attachments; UIs must
// use it consistently so pending echoes match their later user echoes.
func Echo(line string, atts []Attachment) string {
	var sb strings.Builder
	sb.WriteString(line)
	for _, a := range atts {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		if a.Path != "" {
			fmt.Fprintf(&sb, "📎 %s", a.Name)
		} else {
			fmt.Fprintf(&sb, "📎 %s (%d KB)", a.Name, (len(a.Data)+1023)/1024)
		}
	}
	return sb.String()
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
		line, atts, _ := ag.IO.QueueTake()
		ag.IO.UserLine(Echo(line, atts))
		ag.commit(userMessage(line, atts))
	}
}

func (ag *Agent) Replay() { ag.IO.Replay(ag.History) }

func (ag *Agent) LastSleep() (int, string) {
	for i := len(ag.History) - 1; i >= 0; i-- {
		it := parseItem(ag.History[i])
		if it.Type != "function_call" || it.Name != "sleep" {
			continue
		}
		var args struct {
			Seconds int    `json:"seconds"`
			Reason  string `json:"reason"`
		}
		json.Unmarshal([]byte(it.Arguments), &args)
		return min(max(args.Seconds, 60), 24*60*60), args.Reason
	}
	return 60, "scheduled check"
}

const compactPrompt = "Write a handoff summary for another LLM instance " +
	"that will resume this work. Cover: the user's goals and constraints, decisions " +
	"made, current state (including uncommitted changes), and immediate next " +
	"steps. Be terse: plain bullets, under 400 words. Name file paths so the " +
	"next instance can read them, but do not quote file contents, code, " +
	"diffs, or command output — everything on disk is re-readable. Do not " +
	"restate your instructions or AGENTS.md content; the next instance gets " +
	"them again. No headings, no code blocks. Output only the summary."

// compactRatio of the model's context window triggers auto-compaction.
const compactRatio = 0.8

// ctxWindow is the current model's context window, 0 when unknown.
func (ag *Agent) ctxWindow() int64 {
	for _, m := range ag.Prov.Models() {
		if m.Slug == ag.Cfg.Model {
			return m.ContextWindow
		}
	}
	return 0
}

// Compact summarizes the history into a fresh session seeded with the
// summary. The old session file stays on disk as the archive.
func (ag *Agent) Compact(ctx context.Context) error {
	if len(ag.History) == 0 {
		ag.IO.Notice("📭 nothing to compact")
		return nil
	}
	if err := ag.IO.TurnBegin(); err != nil {
		return err
	}
	var sb strings.Builder
	req := append(append([]json.RawMessage{}, ag.History...), userMessage(compactPrompt, nil))
	cb := &provider.Callbacks{
		OnTextDelta:     func(s string) { sb.WriteString(s); ag.IO.TextDelta(s) },
		OnThinkingDelta: ag.IO.ThinkingDelta,
		OnNotice:        ag.IO.Notice,
	}
	cfg := *ag.Cfg // summarizing needs no deep reasoning
	cfg.Effort = "low"
	err := ag.Prov.Turn(ctx, req, json.RawMessage("[]"), &cfg, cb)
	ag.IO.TurnEnd()
	if err != nil {
		return err
	}
	summary := strings.TrimSpace(sb.String())
	if summary == "" {
		return errors.New("compaction returned an empty summary")
	}

	oldID := ag.Cfg.SessionID
	root := ag.Sess.Root
	ag.Cfg.SessionID = uuid.NewString()
	next, err := session.NewCompacted(ag.Cfg, oldID, root)
	if err != nil {
		ag.Cfg.SessionID = oldID
		return errors.New("cannot create session file")
	}
	if ag.Sess.Wake != "" {
		next.SetWake(ag.Sess.Wake)
	}
	ag.Sess.Close()
	*ag.Sess = *next
	ag.History = nil
	ag.commit(userMessage(
		"Summary of the conversation so far (earlier history was compacted):\n\n"+summary, nil))
	ag.IO.Usage(0) // real usage lands after the next request
	ag.IO.Notice(fmt.Sprintf("🗜️  compacted into session %.8s", ag.Cfg.SessionID))
	return nil
}

// maybeCompact auto-compacts when usage crosses the window threshold.
func (ag *Agent) maybeCompact(ctx context.Context) {
	win := ag.ctxWindow()
	if win <= 0 || float64(ag.Sess.Ctx) < compactRatio*float64(win) {
		return
	}
	ag.IO.Notice(fmt.Sprintf("🗜️  context at %d%% — compacting", ag.Sess.Ctx*100/win))
	if err := ag.Compact(ctx); err != nil && !errors.Is(err, provider.ErrInterrupted) {
		ag.IO.Notice(fmt.Sprintf("⚠️  compaction failed: %v", err))
	}
}

// Turn runs one user turn to completion (including tool rounds).
func (ag *Agent) Turn(ctx context.Context, userText string, atts []Attachment) error {
	ag.begin(ctx)
	ag.commit(userMessage(userText, atts))
	return ag.run(ctx)
}

// Retry re-runs the committed history after a failed turn. A failure commits
// nothing, so the same request goes out again with no new user message.
func (ag *Agent) Retry(ctx context.Context) error {
	if len(ag.History) == 0 {
		ag.IO.Notice("📭 nothing to retry")
		return nil
	}
	ag.begin(ctx)
	return ag.run(ctx)
}

// presence tells the model whether anyone is watching, so it can choose
// between answering and using the notify tool.
func presence() string {
	if notify.Watched() {
		return "\n\nThe user is watching."
	}
	return "\n\nThe user is away and cannot see this reply."
}

func (ag *Agent) begin(ctx context.Context) {
	if ag.Cfg.Instructions == "" {
		ag.Cfg.Instructions = instructions.Build(ag.Cfg.Cwd, ag.Cfg.Routine)
	}
	ag.maybeCompact(ctx)
}

// run drives the provider/tool rounds until the model stops calling tools.
func (ag *Agent) run(ctx context.Context) error {
	for {
		var pending []json.RawMessage
		ag.Sess.TurnBegin()
		if err := ag.IO.TurnBegin(); err != nil {
			ag.Sess.TurnEnd()
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
		// Presence can flip mid-turn, so stamp it per request on a copy —
		// the persisted instructions stay free of it.
		cfg := *ag.Cfg
		cfg.Instructions += presence()
		err := ag.Prov.Turn(ctx, ag.History, ag.tools, &cfg, cb)
		ag.IO.TurnEnd()
		if err != nil {
			ag.Sess.TurnFailed()
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
			ag.Sess.TurnEnd()
			return nil
		}
		ag.Sess.ToolsBegin() // calls are durable; tool execution is never auto-retried

		interrupted, endTurn := false, false
		for _, call := range calls {
			if ctx.Err() != nil {
				interrupted = true
			}
			if interrupted {
				// Committed calls must still get outputs or the next request 400s.
				ag.commit(callOutput(call.CallID, "[interrupted by user]"))
			} else if ag.runCall(ctx, call) {
				endTurn = true
			}
		}
		if interrupted {
			ag.Sess.TurnEnd()
			return provider.ErrInterrupted
		}
		ag.Sess.TurnEnd()
		if endTurn {
			ag.tools = tools.Schema(ag.Cfg.Routine != "")
			return nil
		}
		ag.steer()
		ag.maybeCompact(ctx) // history is consistent between tool rounds
	}
}

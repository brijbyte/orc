// Package tools implements the agent's tools. Tool errors are returned as
// model output, never aborted on.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/brijbyte/orc/internal/config"
	"github.com/brijbyte/orc/internal/skills"
)

const (
	outCap        = 20480
	outHead       = 12288
	outTail       = 8192
	bashTimeout   = 60 * time.Second
	readLimit     = 1000
	readSourceMax = 8 << 20
	lineMaxChars  = 500
)

// SchemaJSON is the tool definitions array sent to the provider.
const SchemaJSON = `[{"type":"function","name":"bash",` +
	`"description":"Run a shell command. Set background true for a managed long-running process.",` +
	`"parameters":{"type":"object","properties":{` +
	`"cmd":{"type":"string"},` +
	`"timeout_s":{"type":"integer"},` +
	`"background":{"type":"boolean"}},"required":["cmd"]}},` +
	`{"type":"function","name":"process",` +
	`"description":"Inspect or stop managed background processes.",` +
	`"parameters":{"type":"object","properties":{` +
	`"action":{"type":"string","enum":["list","status","logs","stop"]},` +
	`"id":{"type":"string"},` +
	`"offset":{"type":"integer"}},"required":["action"]}},` +
	`{"type":"function","name":"read",` +
	`"description":"Read file with line numbers.",` +
	`"parameters":{"type":"object","properties":{` +
	`"path":{"type":"string"},` +
	`"offset":{"type":"integer"},` +
	`"limit":{"type":"integer"}},"required":["path"]}},` +
	`{"type":"function","name":"write",` +
	`"description":"Write file, creating dirs.",` +
	`"parameters":{"type":"object","properties":{` +
	`"path":{"type":"string"},` +
	`"content":{"type":"string"}},"required":["path","content"]}},` +
	`{"type":"function","name":"edit",` +
	`"description":"Replace old with new in file. old must match exactly once.",` +
	`"parameters":{"type":"object","properties":{` +
	`"path":{"type":"string"},` +
	`"old":{"type":"string"},` +
	`"new":{"type":"string"}},"required":["path","old","new"]}},` +
	`{"type":"function","name":"skill",` +
	`"description":"Find installed skills. Use when the user asks to find or use a skill.",` +
	`"parameters":{"type":"object","properties":{` +
	`"query":{"type":"string",` +
	`"description":"Search by name or description. Omit to list all skills."}}}},` +
	`{"type":"function","name":"notify",` +
	`"description":"Reach the user away from the UI. Use when long work ends ` +
	`or you need a decision to continue. Delivered only when no UI is open, ` +
	`so never rely on it being seen mid-conversation. Do not use for progress ` +
	`updates or to repeat what you already said.",` +
	`"parameters":{"type":"object","properties":{` +
	`"title":{"type":"string","description":"One short line."},` +
	`"body":{"type":"string","description":"A sentence or two of detail."},` +
	`"urgency":{"type":"string","enum":["info","warn","urgent"]}},` +
	`"required":["title","body"]}}]`

const routineSchemaJSON = `,{"type":"function","name":"sleep",` +
	`"description":"Sleep the routine until a future wake time.",` +
	`"parameters":{"type":"object","properties":{` +
	`"seconds":{"type":"integer"},"reason":{"type":"string"}},` +
	`"required":["seconds","reason"]}},` +
	`{"type":"function","name":"stop",` +
	`"description":"Stop the routine.",` +
	`"parameters":{"type":"object","properties":{` +
	`"reason":{"type":"string"}},"required":["reason"]}}]`

func Schema(routine bool) json.RawMessage {
	if !routine {
		return json.RawMessage(SchemaJSON)
	}
	return json.RawMessage(SchemaJSON[:len(SchemaJSON)-1] + routineSchemaJSON)
}

type RoutineCallbacks struct {
	Sleep func(wake time.Time, reason string)
	Stop  func(reason string)
	now   func() time.Time
}

type args map[string]any

func (a args) str(key string) string {
	s, _ := a[key].(string)
	return s
}

func (a args) num(key string, dflt int) int {
	if v, ok := a[key].(float64); ok {
		return int(v)
	}
	return dflt
}

// clampOutput keeps head + truncation marker + tail within outCap.
func clampOutput(s string) string {
	if len(s) <= outCap {
		return s
	}
	return s[:outHead] +
		fmt.Sprintf("\n...[truncated %d bytes]...\n", len(s)-outHead-outTail) +
		s[len(s)-outTail:]
}

// resolve makes a relative tool path absolute against the session cwd.
func resolve(cwd, path string) string {
	if cwd == "" || path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(cwd, path)
}

// Run executes one tool call in the session cwd; ctx cancellation
// interrupts bash.
func Run(ctx context.Context, cwd, name, argsJSON string) string {
	out, _ := RunWithRoutine(ctx, cwd, name, argsJSON, nil)
	return out
}

// RunWithRoutine also reports when sleep or stop must end the turn.
func RunWithRoutine(ctx context.Context, cwd, name, argsJSON string, routine *RoutineCallbacks) (string, bool) {
	var a args
	if json.Unmarshal([]byte(argsJSON), &a) != nil || a == nil {
		return "error: bad arguments JSON", false
	}
	if routine != nil {
		switch name {
		case "sleep":
			seconds := min(max(a.num("seconds", 60), 60), 24*60*60)
			now := time.Now
			if routine.now != nil {
				now = routine.now
			}
			wake, reason := now().UTC().Add(time.Duration(seconds)*time.Second), a.str("reason")
			if routine.Sleep != nil {
				routine.Sleep(wake, reason)
			}
			return fmt.Sprintf("sleeping until %s — %s", wake.Format(time.RFC3339), reason), true
		case "stop":
			reason := a.str("reason")
			if routine.Stop != nil {
				routine.Stop(reason)
			}
			return "routine stopped — " + reason, true
		}
	}
	var out string
	switch name {
	case "bash":
		out = toolBash(ctx, cwd, a)
	case "process":
		out = clampOutput(processTool(a))
	case "read":
		out = clampOutput(toolRead(cwd, a))
	case "write":
		out = toolWrite(cwd, a)
	case "edit":
		out = toolEdit(cwd, a)
	case "skill":
		out = clampOutput(skills.Query(cwd, a.str("query")))
	case "notify":
		out = toolNotify(ctx, a)
	default:
		out = fmt.Sprintf("error: unknown tool %s", name)
	}
	return out, false
}

func toolBash(ctx context.Context, cwd string, a args) string {
	cmd := a.str("cmd")
	if cmd == "" {
		return "error: missing cmd"
	}
	if b, _ := a["background"].(bool); b {
		return processStart(cwd, cmd)
	}
	timeout := time.Duration(a.num("timeout_s", int(bashTimeout/time.Second))) * time.Second

	c := exec.Command("/bin/sh", "-c", cmd)
	c.Dir = cwd
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out strings.Builder
	c.Stdout = &out
	c.Stderr = &out
	if err := c.Start(); err != nil {
		return fmt.Sprintf("error: spawn failed: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	var waitErr error
	timedOut, interrupted := false, false
	select {
	case waitErr = <-done:
	case <-time.After(timeout):
		timedOut = true
	case <-ctx.Done():
		interrupted = true
	}
	if timedOut || interrupted {
		syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		select {
		case waitErr = <-done:
		case <-time.After(2 * time.Second):
			// Wait outlives the kill when an orphaned child holds the pipe.
			fmt.Fprintf(os.Stderr, "⏳ orc: waiting for %q — a child process still holds its output\n", cmdLabel(cmd))
			waitErr = <-done
		}
	}

	tail := ""
	switch {
	case timedOut:
		tail = fmt.Sprintf("\n[timed out after %ds]", int(timeout/time.Second))
	case interrupted:
		tail = "\n[interrupted]"
	case waitErr != nil:
		if ee, ok := waitErr.(*exec.ExitError); ok {
			ws := ee.Sys().(syscall.WaitStatus)
			if ws.Signaled() {
				tail = fmt.Sprintf("\n[signal %d]", ws.Signal())
			} else {
				tail = fmt.Sprintf("\n[exit %d]", ws.ExitStatus())
			}
		} else {
			tail = fmt.Sprintf("\n[error: %v]", waitErr)
		}
	}
	return clampOutput(out.String() + tail)
}

// cmdLabel shortens a command to one short line for user-facing notes.
func cmdLabel(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if i := strings.IndexByte(cmd, '\n'); i >= 0 {
		cmd = cmd[:i] + " …"
	}
	if len(cmd) > 48 {
		cmd = cmd[:47] + "…"
	}
	return cmd
}

func toolRead(cwd string, a args) string {
	path := resolve(cwd, a.str("path"))
	if path == "" {
		return "error: missing path"
	}
	offset := max(a.num("offset", 0), 0)
	limit := min(max(a.num("limit", readLimit), 0), readLimit)

	f, err := os.Open(path)
	if err != nil {
		return fmt.Sprintf("error: cannot open %s", path)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Sprintf("error: not a regular file: %s", path)
	}
	if info.Size() > readSourceMax {
		return fmt.Sprintf("error: file exceeds %d MB: %s", readSourceMax>>20, path)
	}
	data, err := io.ReadAll(io.LimitReader(f, readSourceMax+1))
	if err != nil {
		return fmt.Sprintf("error: cannot read %s", path)
	}
	if len(data) > readSourceMax {
		return fmt.Sprintf("error: file exceeds %d MB: %s", readSourceMax>>20, path)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var out strings.Builder
	emitted := 0
	for i, line := range lines {
		lineno := i + 1
		if lineno <= offset {
			continue
		}
		if emitted >= limit {
			fmt.Fprintf(&out, "[more lines after %d]\n", lineno-1)
			break
		}
		if len(line) > lineMaxChars {
			line = line[:lineMaxChars] + "..."
		}
		fmt.Fprintf(&out, "%6d\t%s\n", lineno, line)
		emitted++
	}
	if out.Len() == 0 {
		return "(empty)"
	}
	return out.String()
}

func toolWrite(cwd string, a args) string {
	path := resolve(cwd, a.str("path"))
	content, hasContent := a["content"].(string)
	if path == "" || !hasContent {
		return "error: missing path/content"
	}
	if dir := filepath.Dir(path); dir != "." {
		os.MkdirAll(dir, 0o755)
	}
	if config.WriteFileAtomic(path, []byte(content)) != nil {
		return fmt.Sprintf("error: cannot write %s", path)
	}
	return "ok"
}

func toolEdit(cwd string, a args) string {
	path, old, newText := resolve(cwd, a.str("path")), a.str("old"), a.str("new")
	if path == "" || a["old"] == nil || a["new"] == nil {
		return "error: missing path/old/new"
	}
	if old == "" {
		return "error: old is empty"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("error: cannot open %s", path)
	}
	text := string(data)
	count := strings.Count(text, old)
	if count == 0 {
		return "error: old string not found"
	}
	if count > 1 {
		return fmt.Sprintf("error: old matches %d times; add context", count)
	}
	if config.WriteFileAtomic(path, []byte(strings.Replace(text, old, newText, 1))) != nil {
		return fmt.Sprintf("error: cannot write %s", path)
	}
	return "ok"
}

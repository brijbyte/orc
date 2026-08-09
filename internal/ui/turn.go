package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	styleDim      = lipgloss.NewStyle().Faint(true)
	styleBold     = lipgloss.NewStyle().Bold(true)
	styleCyan     = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleBoldCyan = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	styleReverse  = lipgloss.NewStyle().Reverse(true)
	styleRed      = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleGreen    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
)

// turn renders one streaming model turn: thinking lines, then markdown text.
// println emits one finished terminal line; tty gates styling.
type turn struct {
	md           *mdStream
	think        strings.Builder
	thinkingOpen bool
	tty          bool
	started      time.Time
	println      func(string)
}

func newTurn(println func(string), width func() int, tty bool) *turn {
	t := &turn{tty: tty, started: time.Now(), println: println}
	lead := "● "
	if tty {
		lead = styleBold.Render("●") + " "
	}
	t.md = newMDStream(println, width, lead)
	return t
}

func (t *turn) thinkLine(line string) {
	if t.tty {
		line = styleDim.Render(line)
	}
	t.println(line)
}

func (t *turn) thinkFlush() {
	if t.think.Len() > 0 {
		t.thinkLine(t.think.String())
		t.think.Reset()
	}
}

func (t *turn) thinkDone() {
	if !t.thinkingOpen {
		return
	}
	t.thinkingOpen = false
	t.thinkFlush()
	secs := int(time.Since(t.started).Seconds())
	if secs < 1 {
		secs = 1
	}
	line := fmt.Sprintf("✻ thought for %ds", secs)
	if t.tty {
		line = styleDim.Render(line)
	}
	t.println(line)
}

func (t *turn) text(text string) {
	if t.thinkingOpen {
		t.thinkDone()
		t.println("")
	}
	if !t.tty {
		fmt.Print(text)
		return
	}
	t.md.feed(text)
}

func (t *turn) thinking(text string) {
	t.thinkingOpen = true
	if !t.tty {
		fmt.Print(text)
		return
	}
	t.think.WriteString(text)
	for {
		s := t.think.String()
		nl := strings.IndexByte(s, '\n')
		if nl < 0 {
			return
		}
		t.thinkLine(s[:nl])
		t.think.Reset()
		t.think.WriteString(s[nl+1:])
	}
}

func (t *turn) end() {
	t.thinkDone()
	t.thinkFlush()
	if t.tty {
		t.md.end()
	} else {
		fmt.Println()
	}
}

func toolIcon(name string) string {
	switch name {
	case "bash":
		return "💻"
	case "process":
		return "⚙️"
	case "read":
		return "📖"
	case "write":
		return "📝"
	case "edit":
		return "✏️"
	case "skill":
		return "🧠"
	}
	return "🔧"
}

// displayPath shortens absolute paths under cwd.
func displayPath(path string) string {
	if !strings.HasPrefix(path, "/") {
		return path
	}
	full := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		full = resolved
	}
	cwd, err := os.Getwd()
	if err != nil {
		return path
	}
	if full == cwd {
		return "."
	}
	if rel, ok := strings.CutPrefix(full, cwd+"/"); ok {
		return rel
	}
	return path
}

// toolLine formats the dim one-liner shown for a tool call.
func toolLine(name, argsJSON string, tty bool) string {
	var args struct {
		Cmd  string `json:"cmd"`
		Path string `json:"path"`
	}
	json.Unmarshal([]byte(argsJSON), &args)
	desc := args.Cmd
	if desc == "" && args.Path != "" {
		desc = displayPath(args.Path)
	}
	if len(desc) > 100 {
		desc = desc[:100]
	}
	line := fmt.Sprintf("%s %s %s", toolIcon(name), name, desc)
	if tty {
		return styleDim.Render(line)
	}
	return line
}

const diffMax = 20

// editDiff renders a ±preview for the edit tool; "" for other tools.
func editDiff(name, argsJSON string, tty bool) string {
	if name != "edit" {
		return ""
	}
	var a struct{ Old, New string }
	if json.Unmarshal([]byte(argsJSON), &a) != nil || a.Old == "" {
		return ""
	}
	var lines []string
	add := func(prefix, text string, style lipgloss.Style) {
		for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			line := prefix + l
			if tty {
				line = style.Render(line)
			}
			lines = append(lines, line)
		}
	}
	add("  - ", a.Old, styleRed)
	if a.New != "" {
		add("  + ", a.New, styleGreen)
	}
	if len(lines) > diffMax {
		more := fmt.Sprintf("  … %d more lines", len(lines)-diffMax)
		if tty {
			more = styleDim.Render(more)
		}
		lines = append(lines[:diffMax], more)
	}
	return strings.Join(lines, "\n")
}

// userEcho formats a user line for the scrollback.
func userEcho(line string, tty bool) string {
	if tty {
		return styleBoldCyan.Render(">") + " " + styleCyan.Render(line)
	}
	return "> " + line
}

// messageText joins the text parts of a message item.
func messageText(raw json.RawMessage) (role, text string) {
	var msg struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &msg) != nil || msg.Type != "message" {
		return "", ""
	}
	var sb strings.Builder
	for _, part := range msg.Content {
		sb.WriteString(part.Text)
	}
	return msg.Role, sb.String()
}

const replayMax = 30

// replay prints the tail of a resumed session as if it had just streamed.
func replay(history []json.RawMessage, println func(string), width func() int, tty bool) {
	n := len(history)
	if n == 0 {
		return
	}
	start, users := 0, 0
	for i := n - 1; i >= 0 && users < 2; i-- {
		if role, _ := messageText(history[i]); role == "user" {
			start = i
			users++
		}
	}
	if n-start > replayMax {
		start = n - replayMax
	}
	if start > 0 {
		line := fmt.Sprintf("📚 %d earlier items", start)
		if tty {
			line = styleDim.Render(line)
		}
		println(line)
	}
	for i := start; i < n; i++ {
		var probe struct {
			Type      string `json:"type"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}
		if json.Unmarshal(history[i], &probe) != nil {
			continue
		}
		switch probe.Type {
		case "function_call":
			println(toolLine(probe.Name, probe.Arguments, tty))
			if d := editDiff(probe.Name, probe.Arguments, tty); d != "" {
				println(d)
			}
		case "message":
			role, text := messageText(history[i])
			if text == "" {
				continue
			}
			if role == "user" {
				println(userEcho(text, tty))
			} else if tty {
				md := newMDStream(println, width, leadDot(tty))
				md.feed(text + "\n")
				md.end()
			} else {
				for j, line := range strings.Split(text, "\n") {
					if j == 0 {
						line = "● " + line
					}
					println(line)
				}
				println("")
			}
		}
	}
}

func leadDot(tty bool) string {
	if tty {
		return styleBold.Render("●") + " "
	}
	return "● "
}

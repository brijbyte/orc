package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brijbyte/orc/internal/agent"
	"github.com/brijbyte/orc/internal/commands"
	"github.com/brijbyte/orc/internal/config"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// TUI is the interactive terminal UI: a Bubble Tea input line with menu and
// status, scrollback via tea.Println, and the submitted-line queue.
type TUI struct {
	prog  *tea.Program
	q     *queue
	cmds  *commands.Commands
	busy  atomic.Bool
	width atomic.Int32
	turn  *turn

	mu          sync.Mutex
	cancel      context.CancelFunc
	running     atomic.Bool
	pending     string // status set before the program loop runs
	lastPreview string // full text of the last truncated preview (ctrl+o)

	history     []string
	histPos     int
	historyPath string
}

type (
	statusMsg string
	busyMsg   bool
	tickMsg   struct{}
	quitMsg   struct{}
)

func NewTUI() *TUI {
	t := &TUI{q: newQueue(), historyPath: config.Path("history")}
	t.width.Store(int32(termWidth()))
	t.loadHistory()
	m := &uiModel{t: t, input: newInput()}
	t.prog = tea.NewProgram(m)
	return t
}

func (t *TUI) SetCommands(c *commands.Commands) { t.cmds = c }

// Run blocks until the UI quits (driver sent quitMsg or fatal error).
func (t *TUI) Run() error {
	t.running.Store(true)
	_, err := t.prog.Run()
	t.saveHistory()
	return err
}

func (t *TUI) takePending() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pending
}

func (t *TUI) widthFn() int {
	if w := int(t.width.Load()); w > 0 {
		return w
	}
	return 80
}

// println prints one scrollback block. Long styled lines are pre-wrapped
// (glamour); long plain lines (e.g. the login URL) hard-wrap here because
// the renderer clips anything wider than the terminal.
func (t *TUI) println(s string) {
	if !strings.Contains(s, "\x1b") {
		width := t.widthFn() - 1
		var wrapped []string
		for _, line := range strings.Split(s, "\n") {
			r := []rune(line)
			for len(r) > width && width > 0 {
				wrapped = append(wrapped, string(r[:width]))
				r = r[width:]
			}
			wrapped = append(wrapped, string(r))
		}
		s = strings.Join(wrapped, "\n")
	}
	t.prog.Println(s)
}

// --- agent.IO (called from the driver goroutine) ---

func (t *TUI) TurnBegin() error {
	t.turn = newTurn(t.println, t.widthFn, true)
	return nil
}

func (t *TUI) TextDelta(text string)     { t.turn.text(text) }
func (t *TUI) ThinkingDelta(text string) { t.turn.thinking(text) }

func (t *TUI) TurnEnd() {
	if t.turn != nil {
		t.turn.end()
		t.turn = nil
	}
}

func (t *TUI) ToolCall(name, argsJSON string) {
	t.println(ToolLine(name, argsJSON, true))
	short, full := ToolPreview(name, argsJSON, true, "ctrl+o expands")
	if short == "" {
		return
	}
	t.println(short)
	t.mu.Lock()
	if full != short {
		t.lastPreview = full
	} else {
		t.lastPreview = ""
	}
	t.mu.Unlock()
}

// takePreview returns and clears the pending full preview.
func (t *TUI) takePreview() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	full := t.lastPreview
	t.lastPreview = ""
	return full
}

func (t *TUI) UserLine(line string) { t.println(userEcho(line, true)) }
func (t *TUI) Notice(line string)   { t.println(line) }

// Replay before the program starts prints plainly; later (in-session
// /resume) it must go through the program to keep the input line intact.
func (t *TUI) Replay(history []json.RawMessage) {
	out := func(s string) { fmt.Println(s) }
	if t.running.Load() {
		out = t.println
	}
	replay(history, out, t.widthFn, true)
}

func (t *TUI) Usage(tokens int64) {
	if t.cmds != nil {
		t.cmds.CtxUsed(tokens)
	}
}

func (t *TUI) QueueDrain()               {}
func (t *TUI) QueuePeek() (string, bool) { return t.q.peek() }
func (t *TUI) QueueTake() (string, []agent.Attachment, bool) {
	it, ok := t.q.take()
	return it.line, nil, ok
}

// --- commands.UI ---

func (t *TUI) Printf(format string, a ...any) { t.println(fmt.Sprintf(format, a...)) }

// SetStatus before Run would block in Send; park the value for Init instead.
func (t *TUI) SetStatus(s string) {
	if !t.running.Load() {
		t.mu.Lock()
		t.pending = s
		t.mu.Unlock()
		return
	}
	t.prog.Send(statusMsg(s))
}

// --- driver support ---

// WaitTake blocks for the next submitted line; ok=false on EOF.
func (t *TUI) WaitTake() (line string, queued, ok bool) {
	it, ok := t.q.waitTake()
	return it.line, it.queued, ok
}

func (t *TUI) SetBusy(b bool) {
	t.busy.Store(b)
	t.prog.Send(busyMsg(b))
}

func (t *TUI) SetCancel(cancel context.CancelFunc) {
	t.mu.Lock()
	t.cancel = cancel
	t.mu.Unlock()
}

func (t *TUI) Interrupt() {
	t.mu.Lock()
	if t.cancel != nil {
		t.cancel()
	}
	t.mu.Unlock()
}

func (t *TUI) Quit() { t.prog.Send(quitMsg{}) }

// EchoQueued reprints a queued line as it starts running.
func (t *TUI) EchoQueued(line string) { t.println(userEcho(line, true)) }

// --- history ---

func (t *TUI) histAdd(line string) {
	if line == "" || (len(t.history) > 0 && t.history[len(t.history)-1] == line) {
		t.histPos = len(t.history)
		return
	}
	t.history = append(t.history, line)
	if len(t.history) > 200 {
		t.history = t.history[len(t.history)-200:]
	}
	t.histPos = len(t.history)
}

func (t *TUI) loadHistory() {
	f, err := os.Open(t.historyPath)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		t.histAdd(strings.TrimRight(sc.Text(), "\r\n"))
	}
	if sc.Err() != nil {
		return // keep the lines read so far; saveHistory rewrites a clean file
	}
}

func (t *TUI) saveHistory() {
	f, err := os.Create(t.historyPath)
	if err != nil {
		return
	}
	defer f.Close()
	for _, line := range t.history {
		fmt.Fprintln(f, line)
	}
}

// --- model ---

type menuItem struct {
	name    string
	args    string
	desc    string
	insert  string
	isModel bool
}

type uiModel struct {
	t       *TUI
	input   textinput.Model
	menu    []menuItem
	menuSel int
	status  string
	busy    bool
	spin    int
	width   int
}

func newInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.PromptStyle = styleBoldCyan
	ti.CharLimit = 8192
	ti.Focus()
	return ti
}

func (m *uiModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink,
		func() tea.Msg { return statusMsg(m.t.takePending()) })
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *uiModel) setPrompt() {
	if m.busy {
		m.input.Prompt = spinnerFrames[m.spin] + " "
	} else {
		m.input.Prompt = "> "
	}
}

func (m *uiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.t.width.Store(int32(msg.Width))
		m.input.Width = msg.Width - 4
		return m, nil
	case statusMsg:
		m.status = string(msg)
		return m, nil
	case busyMsg:
		m.busy = bool(msg)
		m.spin = 0
		m.setPrompt()
		if m.busy {
			return m, tick()
		}
		return m, nil
	case tickMsg:
		if m.busy {
			m.spin = (m.spin + 1) % len(spinnerFrames)
			m.setPrompt()
			return m, tick()
		}
		return m, nil
	case quitMsg:
		return m, tea.Quit
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *uiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "ctrl+j": // ctrl+j: '\n' from terminals that don't send CR
		return m, m.submit()
	case "tab":
		if len(m.menu) > 0 {
			m.input.SetValue(m.menu[m.menuSel].insert)
			m.input.CursorEnd()
			m.menu = nil
		}
		return m, nil
	case "up":
		if len(m.menu) > 0 {
			m.menuSel = (m.menuSel + len(m.menu) - 1) % len(m.menu)
		} else {
			m.histMove(-1)
		}
		return m, nil
	case "down":
		if len(m.menu) > 0 {
			m.menuSel = (m.menuSel + 1) % len(m.menu)
		} else {
			m.histMove(1)
		}
		return m, nil
	case "esc":
		if len(m.menu) > 0 {
			m.menu = nil
		} else if m.busy {
			m.t.Interrupt()
		}
		return m, nil
	case "ctrl+c":
		m.input.SetValue("")
		m.menu = nil
		if m.busy {
			m.t.Interrupt()
		}
		return m, nil
	case "ctrl+o":
		if full := m.t.takePreview(); full != "" {
			return m, tea.Println(full)
		}
		return m, nil
	case "ctrl+d":
		if m.input.Value() == "" {
			if m.busy {
				m.t.Interrupt()
			}
			m.t.q.setEOF()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.refreshMenu()
	return m, cmd
}

func (m *uiModel) histMove(delta int) {
	t := m.t
	if len(t.history) == 0 {
		return
	}
	pos := t.histPos + delta
	if pos < 0 {
		pos = 0
	}
	if pos > len(t.history) {
		pos = len(t.history)
	}
	t.histPos = pos
	if pos == len(t.history) {
		m.input.SetValue("")
	} else {
		m.input.SetValue(t.history[pos])
	}
	m.input.CursorEnd()
	m.menu = nil
}

func (m *uiModel) submit() tea.Cmd {
	if m.input.Value() == "" {
		return nil
	}
	line := m.input.Value()
	if len(m.menu) > 0 {
		line = m.menu[m.menuSel].insert
	}
	echo := userEcho(line, true)
	if m.busy {
		echo = styleDim.Render("> " + line + " ⏳")
	}
	m.t.histAdd(line)
	m.t.q.push(line, m.busy)
	m.input.SetValue("")
	m.menu = nil
	return tea.Println(echo)
}

// cmdArg returns the partial argument of "<cmd> <partial>" or ok=false.
func cmdArg(value, cmd string) (string, bool) {
	rest, ok := strings.CutPrefix(value, cmd+" ")
	if !ok {
		return "", false
	}
	rest = strings.TrimLeft(rest, " ")
	if strings.ContainsAny(rest, " \t") {
		return "", false
	}
	return rest, true
}

const menuMax = 10

func (m *uiModel) refreshMenu() {
	m.menu = nil
	m.menuSel = 0
	value := m.input.Value()
	if marg, ok := cmdArg(value, "/model"); ok && m.t.cmds != nil {
		for _, mod := range m.t.cmds.Models() {
			if len(m.menu) >= menuMax || !strings.HasPrefix(mod.Slug, marg) {
				continue
			}
			m.menu = append(m.menu, menuItem{name: mod.Slug, desc: mod.Description,
				insert: "/model " + mod.Slug, isModel: true})
		}
		return
	}
	if sarg, ok := cmdArg(value, "/resume"); ok && m.t.cmds != nil {
		for _, s := range m.t.cmds.Sessions() {
			id := s.ID[:min(8, len(s.ID))]
			if len(m.menu) >= menuMax || !strings.HasPrefix(id, sarg) {
				continue
			}
			m.menu = append(m.menu, menuItem{name: id,
				desc: strings.TrimSpace(s.When + "  " + s.Title), insert: "/resume " + id})
		}
		return
	}
	if strings.HasPrefix(value, "/") && !strings.ContainsAny(value, " \t") {
		for _, c := range commands.Cmds {
			if len(m.menu) >= menuMax || !strings.HasPrefix(c.Name, value) {
				continue
			}
			m.menu = append(m.menu, menuItem{name: c.Name, args: c.Args,
				desc: c.Desc, insert: c.Name})
		}
		if m.t.cmds == nil {
			return
		}
		for _, c := range m.t.cmds.CustomCmds() {
			if len(m.menu) >= menuMax || !strings.HasPrefix(c.Name, value) {
				continue
			}
			m.menu = append(m.menu, menuItem{name: c.Name, desc: c.Desc, insert: c.Name})
		}
	}
}

func clipTo(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

func (m *uiModel) View() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	rule := styleDim.Render(strings.Repeat("─", maxInt(1, w-1)))
	var b strings.Builder
	b.WriteString(rule + "\n")
	b.WriteString(m.input.View())

	current := ""
	if m.t.cmds != nil {
		current = m.t.cmds.CurrentModel()
	}
	for i, item := range m.menu {
		b.WriteString("\n")
		var left string
		if item.isModel {
			marker := " "
			if item.name == current {
				marker = "*"
			}
			left = fmt.Sprintf("%s %-25s", marker, item.name)
		} else {
			left = fmt.Sprintf("  %-8s %-16s", item.name, item.args)
		}
		desc := clipTo(item.desc, w-30)
		if i == m.menuSel {
			b.WriteString(styleReverse.Render(left + desc))
		} else {
			b.WriteString(left + styleDim.Render(desc))
		}
	}
	if m.status != "" {
		b.WriteString("\n" + rule + "\n")
		b.WriteString(styleDim.Render(clipTo(m.status, w-1)))
	}
	return b.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

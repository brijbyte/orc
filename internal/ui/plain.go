package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/brijbyte/orc/internal/commands"
	"github.com/brijbyte/orc/internal/config"
	"github.com/brijbyte/orc/internal/session"
	"golang.org/x/term"
)

func stdoutTTY() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

func termWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	return 80
}

// Plain is the agent IO for one-shot (-p) and piped-stdin runs: straight
// writes to stdout, no input line to repaint.
type Plain struct {
	Cmds *commands.Commands
	turn *turn
}

func (p *Plain) TurnBegin() error {
	p.turn = newTurn(func(s string) { fmt.Println(s) }, termWidth, stdoutTTY())
	return nil
}

func (p *Plain) TextDelta(text string)     { p.turn.text(text) }
func (p *Plain) ThinkingDelta(text string) { p.turn.thinking(text) }
func (p *Plain) TurnEnd() {
	if p.turn != nil {
		p.turn.end()
		p.turn = nil
	}
}

func (p *Plain) ToolCall(name, argsJSON string) {
	tty := stdoutTTY()
	fmt.Println(ToolLine(name, argsJSON, tty))
	if short, _ := ToolPreview(name, argsJSON, tty, ""); short != "" {
		fmt.Println(short)
	}
}

func (p *Plain) UserLine(line string) { fmt.Println(userEcho(line, stdoutTTY())) }

func (p *Plain) Replay(history []json.RawMessage) {
	replay(history, func(s string) { fmt.Println(s) }, termWidth, stdoutTTY())
}

func (p *Plain) Usage(tokens int64) {
	if p.Cmds != nil {
		p.Cmds.CtxUsed(tokens)
	}
}

func (p *Plain) Notice(line string)        { fmt.Fprintln(os.Stderr, line) }
func (p *Plain) QueueDrain()               {}
func (p *Plain) QueuePeek() (string, bool) { return "", false }
func (p *Plain) QueueTake() (string, bool) { return "", false }
func (p *Plain) Printf(f string, a ...any) { fmt.Printf(f+"\n", a...) }
func (p *Plain) SetStatus(s string)        {}

// PromptString is the pipe-mode prompt.
func PromptString() string {
	if stdoutTTY() {
		return styleBoldCyan.Render(">") + " "
	}
	return "> "
}

// PrintSessionResumed reports a resumed session on stderr.
func PrintSessionResumed(id string, items int, path string) {
	if term.IsTerminal(int(os.Stderr.Fd())) {
		fmt.Fprintf(os.Stderr, "↩️  orc: resumed %s %s\n",
			styleBold.Render(fmt.Sprintf("%.8s", id)),
			styleDim.Render(fmt.Sprintf("(%d items) %s", items, path)))
	} else {
		fmt.Fprintf(os.Stderr, "↩️  orc: resumed %.8s (%d items) %s\n", id, items, path)
	}
}

// page writes through $PAGER (default `less -RFX`: ANSI ok, quit if it fits)
// when stdout is a TTY; straight to stdout otherwise.
func page(write func(w io.Writer)) {
	if !stdoutTTY() {
		write(os.Stdout)
		return
	}
	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less -RFX"
	}
	cmd := exec.Command("/bin/sh", "-c", pager)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	pipe, err := cmd.StdinPipe()
	if err != nil || cmd.Start() != nil {
		write(os.Stdout)
		return
	}
	write(pipe) // quitting the pager early closes the pipe; writes just error
	pipe.Close()
	cmd.Wait()
}

func PrintSessionList(rows []session.Info) {
	if len(rows) == 0 {
		fmt.Println("📭 no sessions")
		return
	}
	tty := stdoutTTY()
	page(func(w io.Writer) {
		for _, r := range rows {
			if tty {
				fmt.Fprintf(w, "%s  %s  %s\n",
					styleCyan.Render(fmt.Sprintf("%-8s", r.ID[:min(8, len(r.ID))])),
					styleDim.Render(fmt.Sprintf("%-16s", r.When)), r.Title)
			} else {
				fmt.Fprintf(w, "%-8s  %-16s  %s\n", r.ID[:min(8, len(r.ID))], r.When, r.Title)
			}
		}
	})
}

// Banner prints the startup line. showModel is false when a status bar
// below the input already carries model and effort.
func Banner(cfg *config.Config, resumed, showModel bool) {
	prefix := ""
	if resumed {
		prefix = "resumed "
	}
	switch {
	case !stdoutTTY():
		fmt.Printf("🧌 orc %s — %s (%s effort), %ssession %.8s. Ctrl-D or 'exit' to quit.\n",
			config.Version, cfg.Model, cfg.Effort, prefix, cfg.SessionID)
	case showModel:
		fmt.Printf("%s%s — %s%s\n",
			styleBoldCyan.Render("🧌 orc"),
			styleDim.Render(" "+config.Version),
			styleBold.Render(cfg.Model),
			styleDim.Render(fmt.Sprintf(" (%s effort) · %ssession %.8s · Ctrl-D or 'exit' to quit",
				cfg.Effort, prefix, cfg.SessionID)))
	default:
		fmt.Printf("%s%s\n",
			styleBoldCyan.Render("🧌 orc"),
			styleDim.Render(fmt.Sprintf(" %s · %ssession %.8s · Ctrl-D or 'exit' to quit",
				config.Version, prefix, cfg.SessionID)))
	}
}

// PrintLoginHint warns at startup that the provider has no credentials.
func PrintLoginHint(providerName string) {
	fmt.Printf("🔐 orc: no %s login found — run `orc --login` to sign in\n", providerName)
}

// ResumeHint prints the resume tip on exit.
func ResumeHint(id string) {
	line := fmt.Sprintf("💡 orc: resume with `orc --resume %.8s`", id)
	if term.IsTerminal(int(os.Stderr.Fd())) {
		line = styleDim.Render(line)
	}
	fmt.Fprintln(os.Stderr, line)
}

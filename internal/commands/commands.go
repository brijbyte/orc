// Package commands implements slash commands and the status line.
package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/brijbyte/orc/internal/agent"
	"github.com/brijbyte/orc/internal/config"
	"github.com/brijbyte/orc/internal/provider"
	"github.com/brijbyte/orc/internal/session"
	"github.com/google/uuid"
)

type Cmd struct{ Name, Args, Desc string }

var Cmds = []Cmd{
	{"/model", "[slug]", "set or show the model"},
	{"/effort", "low|medium|high", "set reasoning effort"},
	{"/new", "", "start a fresh session"},
	{"/compact", "", "summarize history into a fresh context"},
	{"/resume", "[id]", "switch to another session"},
	{"/status", "", "show session status"},
	{"/login", "", "sign in to the provider (browser OAuth)"},
	{"/help", "", "list commands"},
	{"/quit", "", "exit orc"},
}

// UI receives command output (whole lines) and status text.
type UI interface {
	Printf(format string, a ...any)
	SetStatus(s string)
}

type Commands struct {
	prov         provider.Provider
	cfg          *config.Config
	ui           UI
	models       []provider.Model
	modelsLoaded bool
	ctxUsed      int64
	customOnce   sync.Once
	customList   []CustomCmd
}

func New(prov provider.Provider, cfg *config.Config, ui UI) *Commands {
	return &Commands{prov: prov, cfg: cfg, ui: ui}
}

func (c *Commands) CurrentModel() string { return c.cfg.Model }

func (c *Commands) Models() []provider.Model {
	if !c.modelsLoaded {
		c.models = c.prov.Models()
		c.modelsLoaded = true
	}
	return c.models
}

// modelCtxWindow is the context window of the current model, 0 when unknown.
func (c *Commands) modelCtxWindow() int64 {
	for _, m := range c.Models() {
		if m.Slug == c.cfg.Model {
			return m.ContextWindow
		}
	}
	return 0
}

// gitBranch finds the branch name (or short detached SHA) from the nearest
// .git/HEAD upward.
func gitBranch(dir string) string {
	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			return ""
		}
	}
	for {
		head, err := os.ReadFile(filepath.Join(dir, ".git/HEAD"))
		if err == nil {
			line, _, _ := strings.Cut(string(head), "\n")
			if ref, ok := strings.CutPrefix(line, "ref: refs/heads/"); ok {
				return ref
			}
			if len(line) > 8 {
				return line[:8]
			}
			return line
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func (c *Commands) StatusUpdate() {
	cwd := c.cfg.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	base := filepath.Base(cwd)
	s := fmt.Sprintf("%s · %s · %s", c.cfg.Model, c.cfg.Effort, base)
	if branch := gitBranch(cwd); branch != "" {
		s += fmt.Sprintf(" (%s)", branch)
	}
	if c.ctxUsed > 0 {
		if c.ctxUsed < 10000 {
			s += fmt.Sprintf(" · ctx %.1fk", float64(c.ctxUsed)/1000.0)
		} else {
			s += fmt.Sprintf(" · ctx %dk", c.ctxUsed/1000)
		}
		if win := c.modelCtxWindow(); win > 0 {
			s += fmt.Sprintf(" (%d%%)", (c.ctxUsed*100+win-1)/win) // ceil: never 0%
		}
	}
	c.ui.SetStatus(s)
}

func (c *Commands) CtxUsed(tokens int64) {
	c.ctxUsed = tokens
	c.StatusUpdate()
}

func (c *Commands) cmdNew(ag *agent.Agent) {
	s := ag.Sess
	s.Close()
	if s.Items == 0 {
		os.Remove(s.Path) // nothing was said; drop the file
	}
	ag.History = nil
	c.cfg.SessionID = uuid.NewString()
	next, err := session.New(c.cfg)
	if err != nil {
		c.ui.Printf("❌ orc: cannot create session file")
		return
	}
	*ag.Sess = *next
	c.ui.Printf("✨ new session %.8s", c.cfg.SessionID)
	c.CtxUsed(0) // empty history: reset the context gauge
}

// Dispatch handles a slash command. Returns handled and quit; a non-empty
// prompt (from a custom command) must be run as a model turn by the caller.
func (c *Commands) Dispatch(ag *agent.Agent, line string) (handled, quit bool, prompt string) {
	if !strings.HasPrefix(line, "/") {
		return false, false, ""
	}
	word, rest, _ := strings.Cut(line, " ")
	if w, r, found := strings.Cut(word, "\t"); found {
		word, rest = w, r+" "+rest
	}
	arg := strings.TrimLeft(rest, " \t")

	var cmd *Cmd
	for i := range Cmds {
		if Cmds[i].Name == word {
			cmd = &Cmds[i]
		}
	}
	// Slash lines are never sent to the model; unknown ones just warn.
	if cmd == nil {
		if p, err := c.customPrompt(word, arg); err == nil {
			return true, false, p
		} else if !errors.Is(err, errNoCustom) {
			c.ui.Printf("❌ %s: %v", word, err)
			return true, false, ""
		}
		c.ui.Printf("⚠️  unknown command %s (try /help)", word)
		return true, false, ""
	}

	switch cmd.Name {
	case "/quit":
		return true, true, ""
	case "/help":
		for _, cc := range Cmds {
			c.ui.Printf("  %-8s %-16s %s", cc.Name, cc.Args, cc.Desc)
		}
		for _, cc := range c.CustomCmds() {
			c.ui.Printf("  %-25s %s", cc.Name, cc.Desc)
		}
	case "/new":
		c.cmdNew(ag)
	case "/compact": // interactive drivers intercept this before Dispatch
		c.ui.Printf("⚠️  /compact is unavailable in this mode")
	case "/resume":
		c.cmdResume(ag, arg)
	case "/status":
		c.cmdStatus(ag)
	case "/model":
		c.cmdModel(ag, arg)
	case "/effort":
		c.cmdEffort(ag, arg)
	}
	return true, false, ""
}

// cmdResume switches the live agent to another session; no arg lists them.
func (c *Commands) cmdResume(ag *agent.Agent, arg string) {
	if arg == "" {
		rows, err := session.List(c.cfg.Cwd)
		if err != nil || len(rows) == 0 {
			c.ui.Printf("📭 no sessions")
			return
		}
		for _, r := range rows {
			marker := ' '
			if strings.HasPrefix(c.cfg.SessionID, r.ID) || strings.HasPrefix(r.ID, c.cfg.SessionID) {
				marker = '*'
			}
			c.ui.Printf("%c %-8.8s %-16s %s", marker, r.ID, r.When, r.Title)
		}
		return
	}
	prevID := c.cfg.SessionID
	next, resumed, err := session.Resume(arg, c.cfg)
	if err != nil {
		c.cfg.SessionID = prevID
		c.ui.Printf("❌ %v", err)
		return
	}
	old := ag.Sess
	old.Close()
	if old.Items == 0 && old.Path != next.Path {
		os.Remove(old.Path)
	}
	*ag.Sess = *next
	ag.History = resumed
	if next.Model != "" {
		c.cfg.Model = next.Model
	}
	if next.Effort != "" {
		c.cfg.Effort = next.Effort
	}
	c.ui.Printf("↩️  resumed %.8s (%d items)", c.cfg.SessionID, next.Items)
	ag.Replay()
	c.CtxUsed(next.Ctx)
}

func (c *Commands) cmdStatus(ag *agent.Agent) {
	c.ui.Printf("🧌 orc %s · session %.8s (%d items)", config.Version, c.cfg.SessionID, ag.Sess.Items)
	c.ui.Printf("📄 %s", ag.Sess.Path)
	c.ui.Printf("🤖 %s (%s effort) · provider %s", c.cfg.Model, c.cfg.Effort, c.prov.Name())
	if c.ctxUsed > 0 {
		s := fmt.Sprintf("📊 ctx %d tokens", c.ctxUsed)
		if win := c.modelCtxWindow(); win > 0 {
			s += fmt.Sprintf(" of %dk (%d%%)", win/1000, (c.ctxUsed*100+win-1)/win)
		}
		c.ui.Printf("%s", s)
	}
	cwd := c.cfg.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if branch := gitBranch(cwd); branch != "" {
		cwd += fmt.Sprintf(" (%s)", branch)
	}
	c.ui.Printf("📁 %s", cwd)
}

// Sessions lists resumable sessions for the menu, current one excluded.
func (c *Commands) Sessions() []session.Info {
	rows, _ := session.List(c.cfg.Cwd)
	out := rows[:0]
	for _, r := range rows {
		if !strings.HasPrefix(c.cfg.SessionID, r.ID) && !strings.HasPrefix(r.ID, c.cfg.SessionID) {
			out = append(out, r)
		}
	}
	return out
}

func (c *Commands) cmdModel(ag *agent.Agent, arg string) {
	models := c.Models()
	if arg == "" {
		c.ui.Printf("🤖 model %s (%s effort)", c.cfg.Model, c.cfg.Effort)
		for _, m := range models {
			marker := ' '
			if m.Slug == c.cfg.Model {
				marker = '*'
			}
			c.ui.Printf("  %c %-22s %s", marker, m.Slug, m.Description)
		}
		return
	}
	known := models == nil // no list: trust the user
	for _, m := range models {
		if m.Slug == arg {
			known = true
		}
	}
	c.cfg.Model = arg
	note := ""
	if !known {
		note = " (not in provider's model list)"
	}
	c.ui.Printf("✅ model set to %s%s", arg, note)
	ag.Sess.SetCfg(c.cfg)
	c.saveDefaults()
	c.StatusUpdate()
}

// saveDefaults makes the current model/effort the defaults for new sessions.
func (c *Commands) saveDefaults() {
	config.SaveSettings(config.Settings{Model: c.cfg.Model, Effort: c.cfg.Effort})
}

func (c *Commands) cmdEffort(ag *agent.Agent, arg string) {
	if arg == "" {
		c.ui.Printf("🧠 effort %s", c.cfg.Effort)
		return
	}
	if arg != "low" && arg != "medium" && arg != "high" {
		c.ui.Printf("⚠️  effort must be low, medium, or high")
		return
	}
	c.cfg.Effort = arg
	c.ui.Printf("✅ effort set to %s", arg)
	ag.Sess.SetCfg(c.cfg)
	c.saveDefaults()
	c.StatusUpdate()
}

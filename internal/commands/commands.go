// Package commands implements slash commands and the status line.
package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
func gitBranch() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
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
	cwd, _ := os.Getwd()
	base := filepath.Base(cwd)
	s := fmt.Sprintf("%s · %s · %s", c.cfg.Model, c.cfg.Effort, base)
	if branch := gitBranch(); branch != "" {
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

// Dispatch handles a slash command. Returns handled and quit.
func (c *Commands) Dispatch(ag *agent.Agent, line string) (handled, quit bool) {
	if !strings.HasPrefix(line, "/") {
		return false, false
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
	if cmd == nil {
		// "/tmp/x ..." is a path, not a typo'd command; hand it to the model
		if strings.Contains(word[1:], "/") {
			return false, false
		}
		c.ui.Printf("⚠️  unknown command %s (try /help)", word)
		return true, false
	}

	switch cmd.Name {
	case "/quit":
		return true, true
	case "/help":
		for _, cc := range Cmds {
			c.ui.Printf("  %-8s %-16s %s", cc.Name, cc.Args, cc.Desc)
		}
	case "/new":
		c.cmdNew(ag)
	case "/model":
		c.cmdModel(ag, arg)
	case "/effort":
		c.cmdEffort(ag, arg)
	}
	return true, false
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

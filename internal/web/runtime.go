package web

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"

	"github.com/brijbyte/orc/internal/agent"
	"github.com/brijbyte/orc/internal/commands"
	"github.com/brijbyte/orc/internal/config"
	"github.com/brijbyte/orc/internal/provider"
	"github.com/brijbyte/orc/internal/session"
)

// Runtime is one live session behind the server: its IO, agent, and driver
// loop. ID is the session id at open time and stays stable even when the
// session id moves on (/new, /resume, compaction).
type Runtime struct {
	ID   string
	IO   *IO
	Ag   *agent.Agent
	Cfg  *config.Config
	Cmds *commands.Commands

	done chan struct{}

	gitMu         sync.Mutex
	gitActivityMu sync.Mutex
	gitActivity   []gitActivity
	terminalMu    sync.Mutex
	terminals     map[*terminalSession]struct{}
}

// NewRuntime wires a session into a web IO and starts its driver loop.
func NewRuntime(prov provider.Provider, cfg *config.Config, sess *session.Session,
	resumed []json.RawMessage, replay bool) *Runtime {
	w := NewIO(cfg)
	cmds := commands.New(prov, cfg, w)
	w.SetCommands(cmds)
	ag := agent.New(cfg, prov, sess, resumed, w)
	if replay {
		ag.Replay() // seed the event log so browsers render the history
	}
	if sess.Ctx > 0 {
		cmds.CtxUsed(sess.Ctx)
	}
	cmds.StatusUpdate()
	rt := &Runtime{ID: cfg.SessionID, IO: w, Ag: ag, Cfg: cfg, Cmds: cmds,
		done: make(chan struct{})}
	go rt.loop()
	return rt
}

// loop is the TUI driver loop over the web queue; it ends on quit or Close.
func (rt *Runtime) loop() {
	defer rt.closeTerminals()
	defer close(rt.done)
	w, ag, cmds := rt.IO, rt.Ag, rt.Cmds
	run := func(f func(ctx context.Context) error) {
		ctx, cancel := context.WithCancel(context.Background())
		w.SetCancel(cancel)
		w.SetBusy(true)
		err := f(ctx)
		w.SetBusy(false)
		w.SetCancel(nil)
		cancel()
		if err != nil && !errors.Is(err, provider.ErrInterrupted) {
			w.Printf("❌ orc: %v", err)
		}
	}
	for {
		line, atts, queued, ok := w.WaitTake()
		if !ok {
			break
		}
		if line == "exit" || line == "quit" {
			break
		}
		if queued {
			w.EchoQueued(agent.Echo(line, atts))
		}
		switch strings.TrimSpace(line) {
		case "/login":
			run(func(ctx context.Context) error {
				return ag.Prov.Login(ctx, w.Notice)
			})
			continue
		case "/compact":
			run(ag.Compact)
			continue
		case "/retry":
			run(ag.Retry)
			continue
		}
		if strings.HasPrefix(line, "/") {
			handled, quit, prompt := cmds.Dispatch(ag, line)
			if quit {
				break
			}
			if prompt == "" && handled {
				continue
			}
			line = prompt // custom command: run its prompt as the turn
		}
		run(func(ctx context.Context) error { return ag.Turn(ctx, line, atts) })
	}
	w.Close()
	sess := ag.Sess
	sess.Close()
	if sess.Items == 0 {
		os.Remove(sess.Path) // nothing was said; drop the empty session file
	}
}

func (rt *Runtime) addTerminal(terminal *terminalSession) bool {
	rt.terminalMu.Lock()
	defer rt.terminalMu.Unlock()
	if rt.done != nil {
		select {
		case <-rt.done:
			return false
		default:
		}
	}
	if rt.terminals == nil {
		rt.terminals = map[*terminalSession]struct{}{}
	}
	rt.terminals[terminal] = struct{}{}
	return true
}

func (rt *Runtime) removeTerminal(terminal *terminalSession) {
	rt.terminalMu.Lock()
	delete(rt.terminals, terminal)
	rt.terminalMu.Unlock()
}

func (rt *Runtime) closeTerminals() {
	rt.terminalMu.Lock()
	terminals := make([]*terminalSession, 0, len(rt.terminals))
	for terminal := range rt.terminals {
		terminals = append(terminals, terminal)
	}
	rt.terminals = nil
	rt.terminalMu.Unlock()
	for _, terminal := range terminals {
		_ = terminal.Close()
	}
}

// Close interrupts any running turn, ends the loop, and waits for it.
func (rt *Runtime) Close() {
	rt.closeTerminals()
	rt.IO.Interrupt()
	rt.IO.Close()
	<-rt.done
}

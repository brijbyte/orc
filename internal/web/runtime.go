package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

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

	afterMu     sync.Mutex
	afterTurn   func(*Runtime)
	stopRoutine bool

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

func (rt *Runtime) notifyTurn() {
	rt.afterMu.Lock()
	fn := rt.afterTurn
	rt.afterMu.Unlock()
	if fn != nil {
		fn(rt)
	}
}

func (rt *Runtime) setAfterTurn(fn func(*Runtime)) {
	rt.afterMu.Lock()
	rt.afterTurn = fn
	rt.afterMu.Unlock()
}

func (rt *Runtime) routineError(err error) error {
	ag := rt.Ag
	if !errors.Is(err, provider.ErrInterrupted) && ag.Cfg.Routine != "" {
		seconds, _ := ag.LastSleep()
		delay := time.Duration(min(seconds*2, 24*60*60)) * time.Second
		wake := time.Now().UTC().Add(delay).Format(time.RFC3339)
		ag.Sess.SetWake(wake)
		rt.IO.Notice(fmt.Sprintf("⚠️ routine failed; retrying at %s", wake))
	}
	return err
}

func (rt *Runtime) routineTurn(ctx context.Context, text string, atts []agent.Attachment) error {
	ag := rt.Ag
	if ag.Cfg.Routine != "" && ag.Sess.Wake != "" {
		ag.Sess.SetWake("")
	}
	if err := ag.Turn(ctx, text, atts); err != nil {
		return rt.routineError(err)
	}
	if ag.Cfg.Routine == "" || ag.Sess.Wake != "" {
		return nil
	}
	if err := ag.Turn(ctx, "call sleep or stop", nil); err != nil {
		return rt.routineError(err)
	}
	if ag.Cfg.Routine != "" && ag.Sess.Wake == "" {
		ag.Sess.StopRoutine()
		ag.Cfg.Routine = ""
		rt.IO.Notice("⚠️ routine stopped: sleep or stop was not called")
	}
	return nil
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
		rt.notifyTurn()
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
		case "/wake":
			if ag.Cfg.Routine != "" {
				_, reason := ag.LastSleep()
				run(func(ctx context.Context) error {
					return rt.routineTurn(ctx, "⏰ wake: "+reason, nil)
				})
			}
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
		if ag.Cfg.Routine != "" {
			run(func(ctx context.Context) error { return rt.routineTurn(ctx, line, atts) })
		} else {
			run(func(ctx context.Context) error { return ag.Turn(ctx, line, atts) })
		}
	}
	w.Close()
	rt.afterMu.Lock()
	stopRoutine := rt.stopRoutine
	rt.afterMu.Unlock()
	if stopRoutine && ag.Cfg.Routine != "" {
		ag.Sess.StopRoutine()
		ag.Cfg.Routine = ""
	}
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

func (rt *Runtime) StopRoutine() {
	rt.afterMu.Lock()
	rt.stopRoutine = true
	rt.afterMu.Unlock()
	rt.Close()
}

// Close interrupts any running turn, ends the loop, and waits for it.
func (rt *Runtime) Close() {
	rt.closeTerminals()
	rt.IO.Interrupt()
	rt.IO.Close()
	<-rt.done
}

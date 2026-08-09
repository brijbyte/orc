// orc — minimal coding-agent harness.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/brijbyte/orc/internal/agent"
	"github.com/brijbyte/orc/internal/commands"
	"github.com/brijbyte/orc/internal/config"
	"github.com/brijbyte/orc/internal/provider"
	_ "github.com/brijbyte/orc/internal/provider/codex"
	"github.com/brijbyte/orc/internal/session"
	"github.com/brijbyte/orc/internal/tools"
	"github.com/brijbyte/orc/internal/ui"
	"github.com/google/uuid"
	"golang.org/x/term"
)

func usage() {
	fmt.Println(`usage: orc [options] [-p "prompt"]
  -p <prompt>       one-shot: run a single task and exit
  -m <model>        model (default: provider's; env ORC_MODEL)
  -e <effort>       reasoning effort: low|medium|high (default ` + config.DefaultEffort + `)
  --provider <name> provider (default codex; env ORC_PROVIDER)
  --resume [id|path] resume most recent (or given) session
  --list            list sessions for this directory, newest first
  --login           sign in to the provider (browser OAuth)
  --auth            show provider auth status
  --version         print version
  -h                help`)
}

type options struct {
	prompt    string
	resumeRef string
	doResume  bool
	doList    bool
	doLogin   bool
	doAuth    bool
}

func parseArgs(cfg *config.Config, opts *options) (modelExplicit, effortExplicit bool, err error) {
	args := os.Args[1:]
	need := func(i int, flag string) (string, error) {
		if i+1 >= len(args) {
			return "", fmt.Errorf("option requires an argument: %s", flag)
		}
		return args[i+1], nil
	}
	for i := 0; i < len(args); i++ {
		var v string
		switch a := args[i]; a {
		case "-p", "-m", "-e", "--provider":
			if v, err = need(i, a); err != nil {
				return
			}
			i++
			switch a {
			case "-p":
				opts.prompt = v
			case "-m":
				cfg.Model = v
				modelExplicit = true
			case "-e":
				cfg.Effort = v
				effortExplicit = true
			case "--provider":
				cfg.Provider = v
			}
		case "--resume":
			opts.doResume = true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				opts.resumeRef = args[i+1]
				i++
			}
		case "--list":
			opts.doList = true
		case "--login":
			opts.doLogin = true
		case "--auth":
			opts.doAuth = true
		case "--version":
			fmt.Println("orc " + config.Version)
			os.Exit(0)
		case "-h", "--help":
			usage()
			os.Exit(0)
		default:
			return false, false, fmt.Errorf("unknown option %s", a)
		}
	}
	return
}

func main() { os.Exit(run()) }

func run() int {
	cfg := &config.Config{
		Provider:  os.Getenv("ORC_PROVIDER"),
		Model:     os.Getenv("ORC_MODEL"),
		Effort:    config.DefaultEffort,
		SessionID: uuid.NewString(),
	}
	var opts options
	modelExplicit, effortExplicit, err := parseArgs(cfg, &opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ orc: %v\n", err)
		usage()
		return 2
	}
	modelExplicit = modelExplicit || cfg.Model != ""

	if opts.doList {
		rows, err := session.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ orc: %v\n", err)
			return 1
		}
		ui.PrintSessionList(rows)
		return 0
	}

	prov := provider.Get(cfg.Provider)
	if prov == nil {
		fmt.Fprintf(os.Stderr, "❌ orc: unknown provider '%s'; available:\n", cfg.Provider)
		provider.List()
		return 2
	}
	if cfg.Model == "" {
		cfg.Model = prov.DefaultModel()
	}

	if opts.doLogin {
		if err := prov.Login(); err != nil {
			fmt.Fprintf(os.Stderr, "❌ orc: %v\n", err)
			return 1
		}
		return 0
	}
	if opts.doAuth {
		if err := prov.AuthStatus(); err != nil {
			return 1
		}
		return 0
	}

	var sess *session.Session
	var resumed []json.RawMessage
	if opts.doResume {
		sess, resumed, err = session.Resume(opts.resumeRef, cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ orc: %v\n", err)
			return 1
		}
		ui.PrintSessionResumed(cfg.SessionID, sess.Items, sess.Path)
		// Restore the session's model/effort; explicit flags win.
		if !modelExplicit && sess.Model != "" {
			cfg.Model = sess.Model
		}
		if !effortExplicit && sess.Effort != "" {
			cfg.Effort = sess.Effort
		}
	} else if sess, err = session.New(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "❌ orc: cannot create session file\n")
		return 1
	}
	defer tools.Cleanup()

	rc := 0
	if opts.prompt != "" {
		rc = runOneShot(cfg, prov, sess, resumed, opts.prompt)
	} else if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
		rc = runTUI(cfg, prov, sess, resumed, opts.doResume)
	} else {
		rc = runPipe(cfg, prov, sess, resumed, opts.doResume)
	}

	sess.Close()
	if sess.Items > 0 {
		ui.ResumeHint(cfg.SessionID)
	} else if !opts.doResume {
		os.Remove(sess.Path) // nothing was said; drop the empty session file
	}
	return rc
}

func runOneShot(cfg *config.Config, prov provider.Provider, sess *session.Session,
	resumed []json.RawMessage, prompt string) int {
	plain := &ui.Plain{}
	cmds := commands.New(prov, cfg, plain)
	plain.Cmds = cmds
	ag := agent.New(cfg, prov, sess, resumed, plain)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := ag.Turn(ctx, prompt)
	switch {
	case errors.Is(err, provider.ErrInterrupted):
		return 130
	case err != nil:
		return 1
	}
	return 0
}

func runPipe(cfg *config.Config, prov provider.Provider, sess *session.Session,
	resumed []json.RawMessage, didResume bool) int {
	plain := &ui.Plain{}
	cmds := commands.New(prov, cfg, plain)
	plain.Cmds = cmds
	ag := agent.New(cfg, prov, sess, resumed, plain)
	ui.Banner(cfg, didResume)
	if didResume {
		ag.Replay()
	}

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for {
		fmt.Print(ui.PromptString())
		if !sc.Scan() {
			break
		}
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			break
		}
		if strings.HasPrefix(line, "/") {
			handled, quit := cmds.Dispatch(ag, line)
			if quit {
				break
			}
			if handled {
				continue
			}
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		ag.Turn(ctx, line)
		stop()
	}
	fmt.Println()
	return 0
}

func runTUI(cfg *config.Config, prov provider.Provider, sess *session.Session,
	resumed []json.RawMessage, didResume bool) int {
	t := ui.NewTUI()
	cmds := commands.New(prov, cfg, t)
	t.SetCommands(cmds)
	ag := agent.New(cfg, prov, sess, resumed, t)

	ui.Banner(cfg, didResume)
	if didResume {
		ag.Replay()
	}
	if sess.Ctx > 0 {
		cmds.CtxUsed(sess.Ctx)
	}
	cmds.StatusUpdate()

	go func() {
		defer t.Quit()
		for {
			line, queued, ok := t.WaitTake()
			if !ok {
				return
			}
			if line == "exit" || line == "quit" {
				return
			}
			if queued { // replay so it's clear what runs now
				t.EchoQueued(line)
			}
			if strings.HasPrefix(line, "/") {
				handled, quit := cmds.Dispatch(ag, line)
				if quit {
					return
				}
				if handled {
					continue
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			t.SetCancel(cancel)
			t.SetBusy(true)
			ag.Turn(ctx, line)
			t.SetBusy(false)
			t.SetCancel(nil)
			cancel()
		}
	}()
	if err := t.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ orc: %v\n", err)
		return 1
	}
	return 0
}

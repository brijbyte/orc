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
	"github.com/brijbyte/orc/internal/web"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type options struct {
	prompt    string
	resumeRef string
	serveAddr string
	domain    string
	doResume  bool
	doList    bool
	doLogin   bool
	doAuth    bool
}

func main() {
	var opts options
	var model, effort, providerName string

	root := &cobra.Command{
		Use:           "orc [--resume [id|path]]",
		Short:         "Minimal coding-agent harness",
		Version:       config.Version,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.doResume = cmd.Flags().Changed("resume")
			// `--resume abc` parses the ref as a positional argument.
			if len(args) == 1 {
				if !opts.doResume || opts.resumeRef != "most-recent" {
					return fmt.Errorf("unexpected argument %s", args[0])
				}
				opts.resumeRef = args[0]
			}
			os.Exit(run(&opts, model, effort, providerName,
				cmd.Flags().Changed("effort")))
			return nil
		},
	}
	f := root.Flags()
	f.StringVarP(&opts.prompt, "prompt", "p", "", "one-shot: run a single task and exit")
	f.StringVarP(&model, "model", "m", "", "model (default: provider's; env ORC_MODEL)")
	f.StringVarP(&effort, "effort", "e", config.DefaultEffort, "reasoning effort: low|medium|high")
	f.StringVar(&providerName, "provider", "", "provider (default codex; env ORC_PROVIDER)")
	f.StringVar(&opts.resumeRef, "resume", "", "resume most recent (or given) session")
	f.Lookup("resume").NoOptDefVal = "most-recent"
	f.StringVar(&opts.serveAddr, "serve", "", "serve the session over HTTP (web UI) instead of the TUI")
	f.Lookup("serve").NoOptDefVal = "127.0.0.1:7777"
	f.StringVar(&opts.domain, "domain", "", "with --serve: public domain, TLS via Let's Encrypt on :443")
	f.BoolVar(&opts.doList, "list", false, "list sessions for this directory, newest first")
	f.BoolVar(&opts.doLogin, "login", false, "sign in to the provider (browser OAuth)")
	f.BoolVar(&opts.doAuth, "auth", false, "show provider auth status")
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetVersionTemplate("orc {{.Version}}\n")

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ orc: %v\n", err)
		os.Exit(2)
	}
}

func run(opts *options, model, effort, providerName string, effortExplicit bool) int {
	cfg := &config.Config{
		Provider:  os.Getenv("ORC_PROVIDER"),
		Model:     os.Getenv("ORC_MODEL"),
		Effort:    effort,
		SessionID: uuid.NewString(),
	}
	cfg.Cwd, _ = os.Getwd()
	if providerName != "" {
		cfg.Provider = providerName
	}
	if model != "" {
		cfg.Model = model
	}
	modelExplicit := cfg.Model != ""
	if opts.resumeRef == "most-recent" {
		opts.resumeRef = ""
	}
	// Saved defaults (written by /model and /effort) fill unset values.
	saved := config.LoadSettings()
	if cfg.Model == "" {
		cfg.Model = saved.Model
	}
	if !effortExplicit && saved.Effort != "" {
		cfg.Effort = saved.Effort
	}

	if opts.doList {
		rows, err := session.List(cfg.Cwd)
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
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		err := prov.Login(ctx, func(s string) { fmt.Println(s) })
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ orc: %v\n", err)
			return 1
		}
		return 0
	}
	if opts.doAuth {
		if err := prov.AuthStatus(); err != nil {
			fmt.Fprintf(os.Stderr, "❌ orc: %v\n", err)
			return 1
		}
		return 0
	}

	var sess *session.Session
	var resumed []json.RawMessage
	var err error
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
	if opts.serveAddr != "" {
		rc = runServe(cfg, prov, sess, resumed, opts)
	} else if opts.prompt != "" {
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
	err := ag.Turn(ctx, prompt, nil)
	switch {
	case errors.Is(err, provider.ErrInterrupted):
		return 130
	case err != nil:
		fmt.Fprintf(os.Stderr, "❌ orc: %v\n", err)
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
	ui.Banner(cfg, didResume, true)
	if !prov.Authenticated() {
		ui.PrintLoginHint(prov.Name())
	}
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
		if strings.TrimSpace(line) == "/login" {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			if err := prov.Login(ctx, func(s string) { fmt.Println(s) }); err != nil {
				fmt.Fprintf(os.Stderr, "❌ orc: %v\n", err)
			}
			stop()
			continue
		}
		if strings.TrimSpace(line) == "/compact" {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			if err := ag.Compact(ctx); err != nil && !errors.Is(err, provider.ErrInterrupted) {
				fmt.Fprintf(os.Stderr, "❌ orc: %v\n", err)
			}
			stop()
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
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		if err := ag.Turn(ctx, line, nil); err != nil && !errors.Is(err, provider.ErrInterrupted) {
			fmt.Fprintf(os.Stderr, "❌ orc: %v\n", err)
		}
		stop()
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ orc: stdin: %v\n", err)
		return 1
	}
	fmt.Println()
	return 0
}

// runServe hosts the multi-session web UI: the initial session becomes the
// first runtime; browsers list, open, and start sessions over the API.
func runServe(cfg *config.Config, prov provider.Provider, sess *session.Session,
	resumed []json.RawMessage, opts *options) int {
	srv := web.NewServer(prov, cfg, opts.serveAddr, opts.domain)
	srv.Register(web.NewRuntime(prov, cfg, sess, resumed, opts.doResume))

	url, err := srv.Start(func(s string) { fmt.Println(s) })
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ orc: %v\n", err)
		return 1
	}
	fmt.Printf("🧌 orc %s serving session %.8s\n🌐 %s\n", config.Version, cfg.SessionID, url)
	if !prov.Authenticated() {
		ui.PrintLoginHint(prov.Name())
	}

	// Ctrl-C stops the server and every runtime.
	sig, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	<-sig.Done()
	srv.Shutdown()
	return 0
}

func runTUI(cfg *config.Config, prov provider.Provider, sess *session.Session,
	resumed []json.RawMessage, didResume bool) int {
	t := ui.NewTUI()
	cmds := commands.New(prov, cfg, t)
	t.SetCommands(cmds)
	ag := agent.New(cfg, prov, sess, resumed, t)

	ui.Banner(cfg, didResume, false)
	if !prov.Authenticated() {
		ui.PrintLoginHint(prov.Name())
	}
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
			if strings.TrimSpace(line) == "/login" {
				// Busy state animates the wait and lets Esc/Ctrl-C cancel.
				ctx, cancel := context.WithCancel(context.Background())
				t.SetCancel(cancel)
				t.SetBusy(true)
				err := prov.Login(ctx, t.Notice)
				t.SetBusy(false)
				t.SetCancel(nil)
				cancel()
				if err != nil {
					t.Printf("❌ orc: %v", err)
				}
				continue
			}
			if strings.TrimSpace(line) == "/compact" {
				ctx, cancel := context.WithCancel(context.Background())
				t.SetCancel(cancel)
				t.SetBusy(true)
				err := ag.Compact(ctx)
				t.SetBusy(false)
				t.SetCancel(nil)
				cancel()
				if err != nil && !errors.Is(err, provider.ErrInterrupted) {
					t.Printf("❌ orc: %v", err)
				}
				continue
			}
			if strings.HasPrefix(line, "/") {
				handled, quit, prompt := cmds.Dispatch(ag, line)
				if quit {
					return
				}
				if prompt == "" && handled {
					continue
				}
				line = prompt // custom command: run its prompt as the turn
			}
			ctx, cancel := context.WithCancel(context.Background())
			t.SetCancel(cancel)
			t.SetBusy(true)
			err := ag.Turn(ctx, line, nil)
			t.SetBusy(false)
			t.SetCancel(nil)
			cancel()
			if err != nil && !errors.Is(err, provider.ErrInterrupted) {
				t.Printf("❌ orc: %v", err)
			}
		}
	}()
	if err := t.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ orc: %v\n", err)
		return 1
	}
	return 0
}

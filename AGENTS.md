# orc — agent context

Minimal Go coding-agent harness. Keep the code and model-facing text terse.

## Build and test

- Build: `make` (Go, writes `bin/orc`)
- Smoke tests: `./bin/orc --auth`, `./bin/orc -p "say hi"`,
  `./bin/orc --resume`, `./bin/orc --list`
- Lint: `go vet -mod=mod ./...`
- The C implementation still lives in `src/` (`make c` builds
  `bin/orc-debug`); it is legacy, pending removal. `vendor/` belongs to the C
  build, so Go commands need `-mod=mod`.

## Architecture

- History is one `[]json.RawMessage` of Responses-API input items. Use the
  same format in memory, on the wire, and in session JSONL. Other wire formats
  translate only inside the provider's `Turn()`.
- `internal/provider` defines providers. Implement each provider in its own
  package under `internal/provider/` and register it from `init()`.
- `internal/agent` owns the turn and tool-call loop. Tools are in
  `internal/tools`. `internal/skills` discovers and searches skills for the
  `skill` tool.
- `internal/instructions` builds agent instructions on the first turn, not at
  startup.
- `internal/commands` implements slash commands. `internal/ui` owns the Bubble
  Tea input line, the `/` menu, input queues, agent output (scrollback via
  `tea.Println`), and glamour markdown rendering; `Plain` serves `-p` and
  piped stdin.
- `cmd/orc` is the Cobra CLI and REPL driver. `/model` and `/effort` persist
  defaults for new sessions to `<orc home>/config.json`
  (`internal/config/settings.go`); flags and resumed-session meta win over
  them.

## Codex invariants

- Send `store:false` and the full history. Replay reasoning
  `encrypted_content` without changes.
- orc's `auth.json` holds one section per provider (`{"codex": {...}}`); the
  codex section keeps the Codex CLI token schema, but orc only reads its own
  store — sign in with `orc --login`. Before token refresh, re-read the store;
  write rotated tokens atomically.
- Add a `function_call_output` for every committed `function_call`, including
  interrupted calls, before the next request.

## Conventions

- Keep `go vet` clean; style via lipgloss, emitted only when the stream is a
  TTY.
- Interrupts travel as `context.Context` cancellation: provider requests and
  bash tool runs take ctx; a canceled turn returns `provider.ErrInterrupted`.
- `tea.Program.Send`/`Println` block until the program loop runs; never call
  them before `Run()` (park state and deliver from `Init`).
- Return tool errors as model output. Do not abort the agent.

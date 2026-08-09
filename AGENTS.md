# orc — agent context

Minimal Go coding-agent harness. Keep the code and model-facing text terse.

## Build and test

- Build: `make` (Go, writes `bin/orc`). Web UI: `make web` (Vite build into
  `internal/web/dist`, embedded via go:embed; without it the server shows a
  placeholder page). Rebuild both after web changes.
- Tests: `go test -mod=mod ./internal/ui/`. Lint: `go vet -mod=mod ./...`.
- Smoke: `./bin/orc --auth`, `./bin/orc -p "say hi"`, `--resume`, `--list`,
  `--serve=127.0.0.1:7799` (curl the printed `#token` against
  `/api/sessions`).
- Legacy C implementation in `src/` (`make c`) pending removal; `vendor/`
  belongs to it, which is why Go commands need `-mod=mod`.
- Zero-token UI check: hand-write a session file
  `<orc home>/sessions/<unix-ts>-<id8>.jsonl` (a `{"_meta":{"id":...}}` line,
  then one Responses-API item per line) and `orc --resume <id8>` — replay
  renders previews and markdown without any API call. Drive the web UI with
  playwright-core plus a headless Chromium-family browser.

## Architecture

- History is one `[]json.RawMessage` of Responses-API input items. Use the
  same format in memory, on the wire, and in session JSONL. Other wire formats
  translate only inside the provider's `Turn()`.
- `internal/provider` defines providers. Implement each provider in its own
  package under `internal/provider/` and register it from `init()`.
- `internal/agent` owns the turn and tool-call loop, plus `Compact`
  (summarize into a fresh session file; auto-runs at 80% of the model's
  context window, at turn entry and between tool rounds — the points where
  every committed `function_call` has its output).
- Tools are in `internal/tools`; `internal/skills` backs the `skill` tool;
  `internal/instructions` builds agent instructions on the first turn.
- `internal/commands` implements slash commands (/model /effort /new /compact
  /resume /status /login /help /quit) and custom ones from
  `.agents/commands/*.md` (cwd, then home; body becomes the turn prompt,
  `$ARGUMENTS` substituted). `Dispatch` returns that prompt; drivers run it.
  Drivers intercept `/login` and `/compact` before `Dispatch` (they need
  ctx/busy handling).
- `internal/ui` owns the Bubble Tea TUI (scrollback via `tea.Println`, `/`
  menu, queue), glamour markdown, `Plain` for `-p`/pipe, and the shared
  render helpers: `ToolLine`/`ToolDesc`/`ToolPreview` (edit = ± diff with
  chroma tokens over red/green backgrounds; write and clamped bash = numbered
  highlighted code; truncation at 20 lines, ctrl+o expands the last one).
- `internal/web` is `--serve`, a multi-session server: `Server` holds a
  registry of `Runtime`s (one per live session: `IO` + agent + driver-loop
  goroutine; runtimes stay alive until stopped or shutdown, and drop the
  session file when it closes with zero items). `IO` mirrors `agent.IO` onto
  an append-only SSE event log (hub) and a TUI-style input queue. Routes are
  session-scoped: `/api/sessions` (list all + create),
  `/api/sessions/{id}/open|state|events|input|interrupt`, `DELETE` to stop,
  plus `/api/models` and `/api/dirs` (directory picker, POST creates).
  `input` accepts `{text, files:[{name,type,data(base64)}]}` (24MB cap);
  `agent.userMessage` turns attachments into `input_image` data-URL parts
  (images) or inlined `input_text` (text files), and `agent.Echo` is the one
  display form for a line + its 📎 names — pending echoes must match later
  user echoes. Bearer-token auth on
  every route; plain HTTP binds loopback only, `--domain` adds autocert TLS.
  Frontend lives in `web/` (React 19 + Vite): `api`/`events`/`types` plus
  `Sidebar` (session list grouped by cwd), `SessionView` (one mounted view +
  SSE stream per open session), `DirPicker`,
  `Transcript`/`BlockView`/`Preview`/`InputBar`/`StatusBar`; `theme.ts`
  resolves light/dark/system onto `<html data-theme>`.
- `cmd/orc` is the Cobra CLI and the four drivers (TUI, pipe, one-shot,
  serve). `/model` and `/effort` persist defaults to `<orc home>/config.json`;
  flags and resumed-session meta win over them.
- `config.Config.Cwd` is the session working directory: bash/read/write/edit,
  skills and custom-command discovery, instructions, and the status line all
  use it (never `os.Getwd()`), so one server can host sessions across
  directories. Session meta records it; `Resume` restores it when the
  directory still exists.

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
  TTY (so also absent in tests — assert on chroma/raw ANSI, which emit
  unconditionally).
- Interrupts travel as `context.Context` cancellation: provider requests and
  bash tool runs take ctx; a canceled turn returns `provider.ErrInterrupted`.
- `tea.Program.Send`/`Println` block until the program loop runs; never call
  them before `Run()` (park state and deliver from `Init`).
- Return tool errors as model output. Do not abort the agent.
- Release: push a `vX.Y.Z` tag. CI builds the web UI, cross-compiles, uploads
  tarballs + `checksums.txt` + `install.sh` as release assets, and updates
  the Homebrew tap.

# orc — agent context

Minimal Go coding-agent harness. Keep the code and model-facing text terse.

## Build and test

- Build: `make` (Go, writes `bin/orc`). Web UI: `make web` (Vite build into
  `internal/web/dist`, embedded via go:embed; without it the server shows a
  placeholder page). Rebuild both after web changes.
- Tests: `go test -mod=mod ./internal/ui/`. Lint: `go vet -mod=mod ./...`.
  Format: `make fmt` (gofmt + prettier over `web/`); `make fmt-check` in CI.
- Smoke: `./bin/orc --auth`, `./bin/orc -p "say hi"`, `--resume`, `--list`,
  `--serve=127.0.0.1:7799` (sign in with the printed web password, then curl
  `/api/sessions` with the session cookie).
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
  (write a terse LLM handoff into a fresh session file without repeating
  instructions; auto-runs at 80% of the model's context window, at turn entry
  and between tool rounds — the points where every committed `function_call`
  has its output). Resuming any member of a compaction chain opens its newest
  member.
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
  `/api/sessions/{id}/open|state|events|input|interrupt|retry|pin`, `DELETE`
  to stop, file GET/PUT, and Git status/compare/diff/mutation routes; plus
  `/api/models` and `/api/dirs` (directory picker, POST creates).
  `retry` queues the `/retry` control line (no user echo) so the driver calls
  `agent.Retry`: a failed turn commits nothing, so the same request goes out
  again. The frontend shows it as a "try again" button on the last block when
  that block is an error notice and the session is idle.
  Lists come back pinned first, then by session-file mtime (last turn);
  pins live in `<orc home>/config.json`.
  `input` accepts `{text, files:[{name,type,data(base64)}]}` (24MB cap);
  `agent.userMessage` turns attachments into `input_image` data-URL parts
  (images) or inlined `input_text` (text files), and `agent.Echo` is the one
  display form for a line + its 📎 names — pending echoes must match later
  user echoes. Password login sets a signed HTTP-only session cookie;
  `/terminal` connects xterm through PartySocket to a WebSocket-backed PTY in
  the session cwd and closes its shell with the socket or runtime. The frontend
  bundles its Nerd Font and uses `react-resizable-panels` for desktop and mobile
  splits. A bcrypt hash and cookie key live in the `web` section of `<orc
  home>/auth.json` until `orc password --rotate`; plain HTTP binds loopback
  only, `--domain` adds autocert TLS.
  Frontend lives in `web/` (React 19 + Vite + react-router data router).
  `src/` groups by area: the shell (`main.tsx`, `App.tsx`, `app.css`,
  `preflight.css`) at the root, then `lib/` (`api`, `events`, `types`,
  `store`, `revalidate`, `theme` — no JSX), `ui/` (shared primitives),
  `session/` (one session's view), `sidebar/` (session list + `DirPicker`),
  and `auth/` (`Login`).
  The root layout route loads sessions+models (`rootLoader`; models are
  fetched once per page load, and a dead server is loader data so the 5s
  `useRevalidator` poll can recover), and the `/s/:sid` route's loader
  awaits `store.ensure` so the view mounts pre-seeded (no loading flash).
  Post-open sidebar refreshes coalesce through `lib/revalidate.ts`
  (`revalidateSoon`, debounced router.revalidate). The server falls back to index.html for
  unknown paths; browser authentication uses same-origin cookies.
  `lib/store.ts` holds per-open-tab SSE streams, block state, and scroll
  positions outside the render tree (App `ensure`s every open tab, `drop` on
  tab close); `Sidebar` lists sessions grouped by cwd; `SessionView` mounts
  only for the active session and subscribes to the store via
  useSyncExternalStore, over
  `Transcript`/`BlockView`/`Preview`/`InputBar`/`StatusBar`; `FileDrawer` uses
  `CodeEditor` for revision-checked text edits, diffs, and Markdown preview;
  code and diff editors have change-map scrollbar gutters, with hunk selection inline in diffs.
  `GitDrawer` handles local branch switching/creation, branch comparison,
  staging, commits, discard, and discard undo.
  Browser previews and Markdown code highlight client-side. `lib/theme.ts`
  resolves light/dark/system onto `<html data-theme>`. Overlays and
  popups use Base UI (`@base-ui/react`): `ui/Select.tsx` wraps its Select,
  `ui/Button.tsx` its Tooltip; `DirPicker` is a Dialog, session delete an
  AlertDialog. Base UI popups stay mounted when closed — drive tests by
  visibility, not detachment.
  `ui/Button.tsx` is the app's only button — every clickable control routes
  through it. Its props are the whole vocabulary: `outline` (a committed,
  labelled action), `icon` (square `--hit` tap target), `link` (underlined
  inline affordance), `small` (inline size), `tone`
  (accent/success/danger, which move the `--btn-color`/`--btn-hover` pair),
  and `tip` (tooltip + aria-label). A caller's `className` may set placement
  only — position, margin, reveal-on-hover — never the button's own look, so
  a new look means a new variant here, not local CSS.
  Styling: `app.css` is global only (theme
  tokens, `body`, scrollbars, the shared `button` press); every component
  imports its own `*.module.css`, with `ui/dialog.module.css` shared by the
  two dialogs. Module keyframes are scoped, so a module declares the ones it
  uses. Cross-component hooks are data attributes, not shared classes
  (`data-block` on a transcript block reveals its `CopyButton`), and `chroma`
  stays a literal class because `/hl.css` targets it.
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
- orc's `auth.json` holds provider sections plus web auth
  (`{"codex": {...}, "web": {...}}`); the codex section keeps the Codex CLI
  token schema, but orc only reads its own
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
- `deploy/provision.sh [name]` creates a Hetzner VM (tokens from env or
  `deploy/.env`): tailnet-only via cloud-init,
  latest release as a user service, `tailscale serve` HTTPS.
  `deploy/destroy.sh` deletes it.
- Release: push a `vX.Y.Z` tag. CI builds the web UI, cross-compiles, uploads
  tarballs + `checksums.txt` + `install.sh` as release assets, and updates
  the Homebrew tap.

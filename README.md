# orc

A minimal coding-agent harness in Go. Talks to the OpenAI Codex backend using
your ChatGPT subscription — no API key. Optimized for simplicity and a small
token footprint: terse tools, capped tool outputs, and server-side prompt
caching.

## Install

Prebuilt binaries for macOS (arm64, x86_64) and Linux (x86_64, arm64):

```
curl -fsSL --connect-timeout 5 https://github.com/brijbyte/orc/releases/latest/download/install.sh | sh
```

or `brew install brijbyte/orc/orc`. Installs to `/usr/local/bin` when
writable, else `~/.local/bin`; pin with `ORC_VERSION=…`, redirect with
`ORC_INSTALL_DIR=…`. Then sign in once with `orc --login` (browser OAuth;
tokens live in orc's own config dir and refresh in place).

## Use

```
orc                        # interactive REPL
orc -p "fix the test"      # one-shot, scriptable (exit 130 on Ctrl-C)
orc --resume [id]          # continue the latest session, or by id prefix
orc --list                 # this directory's sessions
orc --serve[=host:port]    # web UI, default 127.0.0.1:7777
orc -m <model> -e high     # model / effort (env: ORC_MODEL, ORC_PROVIDER)
```

The REPL stays responsive while the agent works: Enter queues the next prompt
(`⏳`), Esc or Ctrl-C interrupts, lines get editing and persistent history.
Typing `/` opens a live command menu (Tab completes; `/model` and `/resume`
complete their arguments). Commands: `/model`, `/effort`, `/new`, `/compact`,
`/resume`, `/status`, `/login`, `/help`, `/quit` — plus custom commands from
`.agents/commands/*.md` (the body runs as the prompt, `$ARGUMENTS`
substituted). Slash lines are never sent to the model.

Edit calls show a ± diff, write and long bash calls show numbered
syntax-highlighted previews; truncated previews expand with ctrl+o (TUI) or a
click (web). `/compact` summarizes the conversation into a fresh session;
it also runs automatically at 80% of the model's context window.

Instruction files append to the system prompt when present:
`~/.agents/AGENTS.md`, then `./AGENTS.md` (32 KB cap each). `/model` and
`/effort` persist defaults to `<orc home>/config.json`; flags and a resumed
session's own settings win.

The orc home is `$XDG_CONFIG_HOME/orc` / `~/.config/orc`, falling back to
`~/.orc`. Sessions are append-only JSONL under `<orc home>/sessions/` — one
Responses-API input item per line, so a session file is literally the
conversation the model sees.

## Web UI

`orc --serve` runs the session headless and prints a tokenized URL for the
browser UI: streamed markdown, tool previews, an interactive status bar
(model/effort selects, light/dark/system theme), queueing and interrupt. The
session file is the same JSONL, so `orc --resume` reopens it in the terminal.
For a public machine, `--serve --domain orc.example.com` serves HTTPS on :443
via Let's Encrypt (needs ports 80/443 and DNS pointing at the host); plain
HTTP never binds beyond loopback, and every request needs the URL token.

## Tools exposed to the model

| tool    | behavior                                                         |
| ------- | ---------------------------------------------------------------- |
| bash    | `sh -c`; 60s timeout, or `background:true` for a managed process |
| process | list, inspect logs, or stop managed background processes         |
| read    | line numbers, offset/limit, long lines truncated                 |
| write   | atomic write, creates parent dirs                                |
| edit    | exact string replace; errors unless exactly one match            |
| skill   | search installed skills (`.agents/skills`, `~/.agents/skills`)   |

All tool outputs are clamped to ~20KB (head + tail) before entering history.

## Design

- History is one `[]json.RawMessage` of Responses-API input items — the wire
  format, the in-memory format, and the on-disk format are the same thing.
- `internal/provider` is the provider seam (callbacks + raw JSON only); new
  providers plug in without touching the agent loop.
- Reasoning items (`encrypted_content`) are replayed verbatim across rounds,
  as the Codex backend requires with `store:false`.
- One UI event stream serves both frontends: the TUI prints it, `--serve`
  broadcasts it over SSE to the embedded React app.

## Layout

```
cmd/orc                CLI + drivers          internal/tools     tool dispatch + processes
internal/agent         agentic loop, compact  internal/session   JSONL persistence
internal/provider      provider registry      internal/skills    skill discovery
internal/provider/codex  Codex auth+SSE       internal/commands  slash + custom commands
internal/ui            Bubble Tea TUI, previews, glamour markdown, plain pipe mode
internal/web           --serve: SSE hub, auth, embedded frontend
web                    React 19 + Vite frontend (built into internal/web/dist)
internal/config        paths, atomic writes   internal/instructions  system prompt
```

## Build from source

```
make            # Go ≥ go.mod version; binary at bin/orc
make web        # rebuild the embedded web UI (Node); optional —
                # without it --serve shows a placeholder page
```

The legacy C implementation remains in `src/` (`make c`) until removal; its
fetched deps live in `vendor/`, which is why Go commands take `-mod=mod`.

## Release (maintainers)

Push a tag: `git tag vX.Y.Z && git push origin main vX.Y.Z`. CI builds the
web UI, cross-compiles all four targets (`make dist`, CGO off), uploads
tarballs + `checksums.txt` + `install.sh` to a GitHub Release, and — when the
`TAP_PUSH_TOKEN` secret is set — regenerates the Homebrew formula.

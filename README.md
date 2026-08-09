# orc

A minimal coding-agent harness in Go. Talks to the OpenAI Codex backend using
your ChatGPT subscription — no API key. Optimized for simplicity and a small
token footprint: terse tools, capped tool outputs, and server-side prompt
caching via `prompt_cache_key`.

## Requirements

- macOS or Linux
- A ChatGPT subscription: sign in once with `orc --login` (browser OAuth;
  credentials go to orc's own config dir and are refreshed in place)

## Install

Prebuilt binaries for macOS (arm64, x86_64) and Linux (x86_64, arm64):

```
curl -fsSL --connect-timeout 5 https://github.com/brijbyte/orc/releases/latest/download/install.sh | sh
```

Installs to `/usr/local/bin` when writable, else `~/.local/bin`. Pin a version
with `ORC_VERSION=0.2`, change the destination with `ORC_INSTALL_DIR=...`.

## Web UI

`orc --serve` runs the session headless and prints a tokenized URL for the
browser UI (default `127.0.0.1:7777`; `--serve=host:port` to change). The
session file is the same JSONL, so `orc --resume` later reopens it in the
terminal. For a public machine, `--serve --domain orc.example.com` serves
HTTPS on :443 via Let's Encrypt (needs ports 80/443 and DNS pointing at the
host); plain HTTP never binds beyond loopback. The frontend is embedded in
the binary — `make web` rebuilds it (Node required).

Or with Homebrew:

```
brew install brijbyte/orc/orc
```

## Build from source

```
make            # or: go build -mod=mod -o bin/orc ./cmd/orc
```

Needs Go 1.22+. The binary lands at `bin/orc`. The legacy C implementation is
still in `src/` (`make c` builds `bin/orc-debug`) until its removal; its
fetched deps live in `vendor/`, which is why Go commands take `-mod=mod`.

## Release (maintainers)

Push a tag: `git tag v0.2 && git push origin v0.2`. CI cross-compiles all
four targets on one runner (`make dist`, CGO off; Linux binaries are static),
uploads tarballs + `checksums.txt` to a GitHub Release, and — when the
`TAP_PUSH_TOKEN` secret (a PAT with push access to `brijbyte/homebrew-orc`)
is set — regenerates the Homebrew formula. `make dist` works locally too.

## Use

```
./bin/orc                          # interactive REPL
./bin/orc -p "fix the failing test"  # one-shot, scriptable (exit 130 on Ctrl-C)
./bin/orc --resume                 # continue the most recent session
./bin/orc --resume <id>            # continue by session id (prefix ok)
./bin/orc --list                   # list this directory's sessions
./bin/orc --login                  # sign in with ChatGPT (browser OAuth)
./bin/orc --auth                   # show Codex auth status
./bin/orc -m gpt-5.6-terra -e high # model / reasoning effort (env: ORC_MODEL)
```

The REPL stays responsive while the agent works: keep typing and press Enter
to queue the next prompt (it runs when the current turn finishes, marked
`⏳`), Ctrl-C interrupts the current turn, and lines get editing + history
(Bubble Tea, persisted at `<orc home>/history`). Esc also cancels the running
turn — keeping the typed line, unlike Ctrl-C — but closes an open menu first.
Slash commands: `/model [slug]`, `/effort low|medium|high`, `/new` (fresh
session), `/login` (browser OAuth, Esc cancels), `/help`, `/quit`. Typing `/`
shows a live candidate list under the input line; Up/Down move the selection,
Enter runs it, Tab completes, Esc closes the menu (history recall also keeps
it closed). Bare `/model` lists the provider's models (for codex, from
`~/.codex/models_cache.json`), and `/model <partial>` completes slugs the
same way. Lines starting with `/` are never sent to the model; unknown
commands just warn.

orc appends user instruction files to the system prompt when they exist:
`~/.agents/AGENTS.md` (global), then `./AGENTS.md` in the working directory
(project). Each file is clamped to 32 KB.

`/model` and `/effort` also persist their values to `<orc home>/config.json`
as the defaults for new sessions; `-m`/`-e` flags and a resumed session's own
settings take precedence.

The orc home is `$XDG_CONFIG_HOME/orc` (or `~/.config/orc` when `~/.config`
exists), falling back to `~/.orc`. Sessions are append-only JSONL under
`<orc home>/sessions/` — one Responses-API input item per line, so a session
file is literally the conversation the model sees.

## Tools exposed to the model

| tool  | behavior                                                              |
| ----- | --------------------------------------------------------------------- |
| bash    | `sh -c`; 60s timeout, or `background:true` for a managed process |
| process | list, inspect logs, or stop managed background processes          |
| read    | line numbers, offset/limit, long lines truncated                  |
| write   | atomic write, creates parent dirs                                 |
| edit    | exact string replace; errors unless exactly one match             |
| skill   | search installed skills (`.agents/skills`, `~/.agents/skills`)    |

All tool outputs are clamped to ~20KB (head + tail) before entering history.

## Design

- History is one `[]json.RawMessage` of Responses-API input items — the wire
  format, the in-memory format, and the on-disk format are the same thing.
- `internal/provider` is the provider seam (callbacks + raw JSON only); an
  Anthropic Messages API provider can be added without touching the agent
  loop.
- Reasoning items (`encrypted_content`) are replayed verbatim across rounds,
  as the Codex backend requires with `store:false`.
- Markdown in replies renders through glamour when stdout is a TTY; raw text
  otherwise. Interrupts travel as context cancellation.

## Layout

```
cmd/orc                CLI, REPL driver       internal/tools     tool dispatch + processes
internal/agent         agentic loop           internal/session   JSONL persistence
internal/provider      provider registry      internal/skills    skill discovery
internal/provider/codex  Codex auth+SSE       internal/commands  slash commands
internal/ui            Bubble Tea TUI, glamour markdown, plain pipe mode
internal/config        paths, atomic writes   internal/instructions  system prompt
```

New providers: one package under `internal/provider/` implementing
`provider.Provider`, registered from `init()`, imported for side effects in
`cmd/orc`.

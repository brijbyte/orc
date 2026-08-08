# orc

A minimal coding-agent harness in C (~2,200 lines). Talks to the OpenAI Codex
backend using your ChatGPT subscription — no API key. Optimized for simplicity
and a small token footprint: 4 terse tools, a ~120-token system prompt, capped
tool outputs, and server-side prompt caching via `prompt_cache_key`.

## Requirements

- macOS or Linux with libcurl (preinstalled on macOS)
- A ChatGPT subscription: sign in once with `orc --login` (browser OAuth;
  credentials go to orc's own config dir). If the Codex CLI is already logged
  in, orc can also reuse `~/.codex/auth.json` as a fallback — tokens are
  refreshed in place either way (rotated refresh tokens are persisted so the
  Codex CLI keeps working)

## Install

Prebuilt binaries for macOS (arm64, x86_64) and Linux (x86_64, arm64; fully
static musl — runs on any distro):

```
curl -fsSL https://raw.githubusercontent.com/brijbyte/orc/main/install.sh | sh
```

Installs to `/usr/local/bin` when writable, else `~/.local/bin`. Pin a version
with `ORC_VERSION=0.2`, change the destination with `ORC_INSTALL_DIR=...`.

Or with Homebrew:

```
brew install brijbyte/orc/orc
```

## Build from source

```
make
```

The first build fetches pinned deps into `vendor/` (gitignored): cJSON
v1.7.18, md4c 0.5.2, utf8proc 2.11.3, c-timestamp, and curl 8.11.1 +
mbedTLS 3.6.2. curl and mbedTLS compile once and link statically, so the binary
depends only on libc/pthread and the OS CA bundle — no system libcurl needed.
`make SYSTEM_CURL=1` links the system libcurl instead (faster first build).
The binary lands at `bin/orc-debug`; `make install` copies it to
`$PREFIX/bin/orc` (default `/usr/local`). macOS builds target macOS 13 by
default; override with `MACOSX_DEPLOYMENT_TARGET=<version> make`.

## Release (maintainers)

Push a tag: `git tag v0.2 && git push origin v0.2`. CI builds all four
targets, uploads tarballs + `checksums.txt` to a GitHub Release, and — when
the `TAP_PUSH_TOKEN` secret (a PAT with push access to
`brijbyte/homebrew-orc`) is set — regenerates the Homebrew formula.

## Use

```
./bin/orc-debug                          # interactive REPL
./bin/orc-debug -p "fix the failing test"  # one-shot, scriptable (exit 130 on Ctrl-C)
./bin/orc-debug --resume                 # continue the most recent session
./bin/orc-debug --resume <id>            # continue by session id (prefix ok)
./bin/orc-debug --list                   # list sessions (id, time, first prompt)
./bin/orc-debug --login                  # sign in with ChatGPT (browser OAuth)
./bin/orc-debug --auth                   # show Codex auth status
./bin/orc-debug -m gpt-5.6-terra -e high # model / reasoning effort (env: ORC_MODEL)
```

The REPL stays responsive while the agent works: keep typing and press Enter
to queue the next prompt (it runs when the current turn finishes, marked
`↳ queued`), Ctrl-C interrupts the current turn, and lines get editing +
history (linenoise, persisted at `<orc home>/history`). Esc also cancels the
running turn — keeping the typed line, unlike Ctrl-C — but closes an open
menu first. Shift+Enter (in
terminals that send CSI-u or modifyOtherKeys encodings) or Ctrl-J inserts a
soft line break, shown inline as `[\n]`; multi-line pastes are folded the
same way.

Slash commands: `/model [slug]`, `/effort low|medium|high`, `/new` (fresh
session), `/help`, `/quit`. Typing `/` shows a live candidate list under the
input line; Up/Down move the selection, Enter runs it, Tab completes, Esc
closes the menu (history recall also keeps it closed). Bare
`/model` lists the provider's models (for
codex, from `~/.codex/models_cache.json`), and `/model <partial>` completes
slugs the same way. Lines that only look like paths (`/tmp/x ...`) still go
to the model.

orc appends user instruction files to the system prompt when they exist:
`~/.agents/AGENTS.md` (global), then `./AGENTS.md` in the working directory
(project). Each file is clamped to 32 KB.

The orc home is `$XDG_CONFIG_HOME/orc` (or `~/.config/orc` when `~/.config`
exists), falling back to `~/.orc`. Sessions are append-only JSONL under
`<orc home>/sessions/` — one Responses-API input item per line, so a session
file is literally the conversation the model sees. `ORC_DEBUG=1` tees raw SSE
to `<orc home>/debug.log`.

## Tools exposed to the model

| tool  | behavior                                                              |
| ----- | --------------------------------------------------------------------- |
| bash  | `sh -c`, own process group, 60s default timeout, stdout+stderr merged |
| read  | line numbers, offset/limit, long lines truncated                      |
| write | atomic write, creates parent dirs                                     |
| edit  | exact string replace; errors unless exactly one match                 |

All tool outputs are clamped to ~20KB (head + tail) before entering history.

## Design

- History is one cJSON array of Responses-API input items — the wire format,
  the in-memory format, and the on-disk format are the same thing.
- `provider.h` is the provider seam (callbacks + cJSON only); an Anthropic
  Messages API provider can be added without touching the agent loop.
- Reasoning items (`encrypted_content`) are replayed verbatim across rounds,
  as the Codex backend requires with `store:false`.
- Markdown in replies is styled with ANSI (headers, bold, italic, `code`,
  links, dim code fences) when stdout is a TTY; raw text otherwise.

## Layout

```
src/main.c            CLI, REPL, signals      src/tools.c    bash/read/write/edit
src/agent.c           agentic loop            src/session.c  JSONL persistence
src/provider.c        provider registry       src/render.c   ANSI markdown
src/providers/codex.c Codex auth+request+SSE  src/input.c    async line editor
src/http.c            libcurl + SSE framing   src/util.c     strbuf, base64url, ...
                                              vendor/        cJSON, md4c, ...
```

New providers: one file in `src/providers/` implementing the `provider`
struct from `src/provider.h`, plus a registry entry in `src/provider.c`.

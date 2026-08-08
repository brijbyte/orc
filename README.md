# orc

A minimal coding-agent harness in C (~2,200 lines). Talks to the OpenAI Codex
backend using your ChatGPT subscription — no API key. Optimized for simplicity
and a small token footprint: 4 terse tools, a ~120-token system prompt, capped
tool outputs, and server-side prompt caching via `prompt_cache_key`.

## Requirements

- macOS or Linux with libcurl (preinstalled on macOS)
- Codex CLI logged in once (`codex login`) — orc reuses `~/.codex/auth.json`
  and refreshes tokens itself (compatible write-back; rotated refresh tokens
  are persisted so the Codex CLI keeps working)

## Build

```
make
```

The first build fetches pinned deps into `vendor/` (gitignored): cJSON
v1.7.18, md4c 0.5.2, c-timestamp, and curl 8.11.1 + mbedTLS 3.6.2. curl and
mbedTLS compile once (a few minutes) and link statically, so the binary
depends only on libc/pthread and the OS CA bundle — no system libcurl needed.
`make SYSTEM_CURL=1` links the system libcurl instead (faster first build).
The binary lands at `bin/orc`. macOS builds target macOS 13 by default;
override with `MACOSX_DEPLOYMENT_TARGET=<version> make`.

## Use

```
./bin/orc                          # interactive REPL
./bin/orc -p "fix the failing test"  # one-shot, scriptable (exit 130 on Ctrl-C)
./bin/orc --resume                 # continue the most recent session
./bin/orc --resume <id>            # continue by session id (prefix ok)
./bin/orc --auth                   # show Codex auth status
./bin/orc -m gpt-5.6-terra -e high # model / reasoning effort (env: ORC_MODEL)
```

The REPL stays responsive while the agent works: keep typing and press Enter
to queue the next prompt (it runs when the current turn finishes, marked
`↳ queued`), Ctrl-C interrupts the current turn, and lines get editing +
history (linenoise, persisted at `<orc home>/history`).

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

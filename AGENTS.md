# orc — agent context

Minimal C coding-agent harness. Keep the code and model-facing text terse.

## Build and test

- Fast build: `make SYSTEM_CURL=1`
- Release-style static build: `make`
- Smoke tests: `./bin/orc --auth`, `./bin/orc -p "say hi"`, `./bin/orc --resume`

## Architecture

- History is one cJSON array of Responses-API input items. Use the same format
  in memory, on the wire, and in session JSONL. Other wire formats translate
  only inside the provider's `turn()`.
- `src/provider.h` defines providers. Implement each provider in
  `src/providers/` and register it in `src/provider.c`.
- `src/agent.c` owns the turn and tool-call loop. Tools are in `src/tools.c`.
- `src/commands.c` implements slash commands. `src/input.c` owns the REPL
  line, the `/` menu, and the spinner.

## Codex invariants

- Send `store:false` and the full history. Replay reasoning
  `encrypted_content` without changes.
- orc's `auth.json` (src/auth.c) holds one section per provider
  (`{"codex": {...}}`); the codex section keeps the Codex CLI schema. Load
  order: orc's store, then flat `~/.codex/auth.json` as fallback. Before token
  refresh, re-read the active auth file; write rotated tokens atomically.
- Add a `function_call_output` for every committed `function_call`, including
  interrupted calls, before the next request.

## Conventions

- Use C11 and keep `-Wall -Wextra` clean.
- `vendor/` is gitignored and fetched at build time. To change vendored
  linenoise, edit `vendor/linenoise.*` and regenerate
  `scripts/linenoise-orc.patch` (the Makefile applies it after fetch).
- Spell ANSI escapes in `src/ansi.h`. Emit styles only when the stream is a
  TTY. Prompts may contain SGR escapes; linenoise skips them in width math.
- Terminal output during a turn must use whole lines between `input_erase()`
  and `input_redraw()`.
- Return tool errors as model output. Do not abort the agent.

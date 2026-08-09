# orc — agent context

Minimal C coding-agent harness. Keep the code and model-facing text terse.

## Build and test

- Fast build: `make SYSTEM_CURL=1`
- Release build: `make release`
- Smoke tests: `./bin/orc-debug --auth`, `./bin/orc-debug -p "say hi"`,
  `./bin/orc-debug --resume`

## Architecture

- History is one cJSON array of Responses-API input items. Use the same format
  in memory, on the wire, and in session JSONL. Other wire formats translate
  only inside the provider's `turn()`.
- `src/provider.h` defines providers. Implement each provider in
  `src/providers/` and register it in `src/provider.c`.
- `src/agent.c` owns the turn and tool-call loop. Tools are in `src/tools.c`.
  `src/skills.c` discovers and searches skills for the `skill` tool.
- `src/instructions.c` builds agent instructions on the first turn, not at
  startup.
- `src/commands.c` implements slash commands. `src/ui.c` owns ncurses input,
  the `/` menu, input queues, agent output, and terminal rendering.

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
- `vendor/` is gitignored and fetched at build time.
- Spell ANSI escapes in `src/ansi.h`. Emit styles only when the stream is a
  TTY.
- Terminal output during a turn must use whole lines between
  `ui_output_suspend()` and `ui_output_resume()`.
- Return tool errors as model output. Do not abort the agent.

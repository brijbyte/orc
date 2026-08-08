# orc — agent context

Minimal coding-agent harness in C. Goals: simplicity and minimal token usage
by the harness. See README.md for user-facing docs.

## Build & test

- `make` (first build fetches pinned cJSON v1.7.18 + md4c 0.5.2 into
  gitignored `vendor/`; binary lands at `bin/orc`)
- Smoke tests: `./bin/orc --auth`, `./bin/orc -p "say hi"`, `./bin/orc --resume`
- `ORC_DEBUG=1` tees raw SSE to `~/.orc/debug.log`

## Architecture

- **History is one cJSON array of Responses-API input items** — the same
  format in memory, on the wire, and on disk (session JSONL). Never introduce
  a translation layer; providers with different wire formats translate only
  inside their own `turn()`.
- **Provider seam**: `src/provider.h` defines `provider` (name, default_model,
  `turn()`, `auth_status()`). Registry in `src/provider.c`. One file per
  provider under `src/providers/` (currently `codex.c`). To add a provider:
  new file implementing the struct, add extern + entry to the registry array.
- `src/agent.c` owns the loop: user msg → `prov->turn()` (streams via
  callbacks) → commit items → run `function_call`s → send outputs → repeat.
- Tools (`src/tools.c`): bash/read/write/edit; schemas are one terse compile-time
  JSON string; all outputs clamped to ~20KB head+tail.

## Codex provider gotchas (src/providers/codex.c)

- Endpoint `chatgpt.com/backend-api/codex/responses`; `store:false` required;
  full history resent each request; `reasoning` items (`encrypted_content`)
  must be replayed verbatim or the backend 400s.
- Auth reuses `~/.codex/auth.json` (Codex CLI). Refresh rotates the
  refresh_token — always re-read auth.json just before refreshing and
  atomic-write back, or the user's Codex CLI login breaks permanently.
- Every committed `function_call` must get a `function_call_output` (even
  "[interrupted]") before the next request.
- Model slugs churn; defaults live on the provider struct. Available slugs:
  `~/.codex/models_cache.json`.

## Conventions

- C11, `-Wall -Wextra` clean. Only deps: system libcurl + vendored cJSON/md4c.
- Tool/model-facing text stays terse (token budget is a feature).
- Errors from tools return as output strings for the model, never abort.

# AGENTS.md — tt

`tt` ("tmux team") is a single-file bash tool: per-project tmux session +
a pool of `pi` code workers. This file orients an AI agent working **on**
`tt` itself.

## Read first

1. `docs/STATUS.md` — current state, the 4 bugs already fixed, what is
   tested, what is not. **Always read before editing.**
2. `docs/DESIGN.md` — design and rationale (why pi windows are shells, how
   the watermark works).
3. `README.md` — user-facing usage and command reference.

## Layout

- `tt` — the entire tool. Pure bash, `set -euo pipefail`. ~552 lines.
- `~/.local/bin/tt` is a **symlink** to `./tt` — edits here take effect
  immediately, no install step.
- `docs/` — design, status, handoff.

## Conventions & invariants — do not regress

- **`set-option` targets use the bare-name `"$s:"` form**, not `"=$s"`.
  The `=` exact-match prefix is rejected by `set-option`.
- **Never `tmux attach` when `$TMUX` is set** — use `switch-client` via
  `enter_session()`.
- **The watermark counts the last non-blank line**, never `wc -l` —
  `capture-pane` pads with blank lines to the pane height.
- **`send` must confirm pi has launched before returning** — `send-keys`
  is asynchronous.
- Pi windows are **plain shells**; `tt pi send` invokes `pi -p ... < file`.
  Do not turn them into a live pi REPL (input-quoting hell — see DESIGN).
- `alfa`/`bravo`/`charlie` are immortal; hard cap of 5 pi workers.

## Testing

There is no test harness — verification is manual against a throwaway
`/tmp/tt-test-*` project. The procedure and the 11-step checklist are in
`docs/STATUS.md`. Live `pi` steps spend OpenAI Codex quota — keep test
tasks trivial. After syntax changes always run `bash -n tt`.

## Consumers

`bassaudio-storefront` is the first adopter:
`.agents/skills/delegating-to-pi/SKILL.md` and its `AGENTS.md` / `CLAUDE.md`
tell the orchestrator to delegate via `tt pi send` / `tt pi wait`. If you
change the `tt pi` interface, update that skill too.

## Commit etiquette

Commit only when the user asks. Keep `tt` a single file.

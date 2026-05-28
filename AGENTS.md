# AGENTS.md — tt

`tt` ("tmux team") is a single-file bash tool: per-project tmux session +
a pool of `pi` code workers. This file orients an AI agent working **on**
`tt` itself.

## Read first

1. `docs/STATUS.md` — current state, what is tested, what is not.
   **Always read before editing.**
2. `docs/DESIGN.md` — design and rationale (the live-REPL model and the
   `tt-worker` extension control channel).
3. `README.md` — user-facing usage and command reference.

## Layout

- `tt` — the tool itself. Pure bash, `set -euo pipefail`.
- `pi-worker/` — worker-only pi runtime templates. `extensions/tt-worker.ts`
  is the pi extension `tt` drives the REPLs through, auto-loaded by pi from
  tt's private `PI_CODING_AGENT_DIR`.
- `~/.local/bin/tt` is a **symlink** to `./tt` — edits here take effect
  immediately, no install step.
- `docs/` — design, status, handoff.

## Conventions & invariants — do not regress

- **`set-option` targets use the bare-name `"$s:"` form**, not `"=$s"`.
  The `=` exact-match prefix is rejected by `set-option`.
- **Never `tmux attach` when `$TMUX` is set** — use `switch-client` via
  `enter_session()`.
- **Pi windows host a live interactive pi REPL**, not a shell. `tt`
  drives them via the `tt-worker` extension's trigger/result files —
  never by `capture-pane` scraping (the retired watermark model was the
  source of every hard bug). See DESIGN.
- **REPL liveness is detected with `pgrep -f` on the worker's
  `--session-dir`**, never `pane_current_command` — pi is a grandchild
  process and tmux reports the foreground command inconsistently.
- **`pi-worker/extensions/tt-worker.ts` must stay inert unless `TT_WORKER_CS` is set** — this
  is a safety belt even though workers now use a private pi worker dir.
- **The `delegating-to-pi` skill must stay excluded from pi workers** —
  `pi-worker/settings.json` excludes it, and `tt` launches worker REPLs
  with `--no-skills` so project/user-discovered skills cannot make a
  delegate become the orchestrator.
- `alfa`/`bravo`/`charlie` are immortal; hard cap of 5 pi workers.

## Testing

There is no test harness — verification is manual against a throwaway
`/tmp/tt-test-*` project. The procedure is in `docs/STATUS.md`. Live
`pi` steps spend OpenAI Codex quota — keep test tasks trivial. After
syntax changes always run `bash -n tt`.

## Consumers

Consumer projects reference `tt` via a `delegating-to-pi` skill
(`SKILL.md`, `AGENTS.md`, `CLAUDE.md`) that tells the orchestrator to
delegate via `tt pi send` / `tt pi wait`. If you change the `tt pi`
interface, update that skill too.

## AI quick reference

```sh
tt pi send alfa <(cat <<'P'
TASK: ...
FILES: path/to/file
CHANGE: ...
SUCCESS: ...
P
)                              # dispatch; prints task-id like "alfa-3"
tt pi wait alfa alfa-3         # block until WORKER_DONE / BLOCKED
tt pi status                   # show all workers: state, last task, tier, gen
tt pi clear alfa               # wipe context; required before reuse
bash -n tt                     # syntax-check after editing tt
```

Worker states: `idle` · `busy` · `blocked` · `interrupted` · `starting` · `down` · `missing`

## Commit etiquette

Commit only when the user asks. Keep `tt` a single file.

## When changing X, update Y

| Change | Also update |
|--------|-------------|
| `tt pi` verbs or flags | `README.md` command table · `tt --help` block · consumer skill `SKILL.md` |
| trigger/result file format or nonce protocol | `docs/DESIGN.md` control channel + task IDs sections |
| worker states | `docs/DESIGN.md` worker state detection section |
| install layout (`~/.local/share/tt/`, symlinks) | `docs/STATUS.md` current state |
| completion markers (`WORKER_DONE` / `BLOCKED`) | `docs/DESIGN.md` · consumer skill `SKILL.md` · `pi-worker/APPEND_SYSTEM.md` |
| model tiers or provider | `docs/DESIGN.md` model tiers · `README.md` |

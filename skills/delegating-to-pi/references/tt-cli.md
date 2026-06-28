# tt CLI — worker pool mechanics

Operational cheat sheet for dispatching pi workers after `SKILL.md` says to
delegate. Drive workers only through `tt pi` verbs — never call `pi` directly.

Workers are live pi REPLs in tmux windows. They spawn lazily, persist unless
removed, and are capped at `min(cores-2, 26)`.

## Choose the worker/flavor

- **Default independent task:** `tt pi auto --rm -` — fresh ephemeral worker,
  auto-reaped after completion; no context leak.
- **Reuse any idle worker:** `tt pi auto -` — picks idle, spawns if needed, or
  queues on the shared pool when at cap.
- **Fresh persistent worker:** `tt pi auto --prefer-fresh -` — useful for fan-out
  or when old context could bias results, but you want the worker to remain.
- **Continue a specific context:** `tt pi send <cs> -` — lazy-spawns if absent and
  queues behind that worker's current turn.
- **Inspect pool:** `tt pi status`.

Tier flags (`--low`/`--medium`/`--high`/`--xhigh`) are **rejected** — thinking
effort is fixed per tier, not independently settable. Pick a model preset with
`--tier NAME` on `send`/`auto`:

- `--tier deepseek` (default) — `opencode-go/deepseek-v4-flash` at xhigh effort.
  Cost-efficient default for high-volume, structured work.
- `--tier minimax` — `opencode-go/minimax-m3` at high effort. Premium tier for
  harder or longer-horizon work; positioned above `deepseek` even at lower
  effort, because the model's higher base capability earns its way.

Omit `--tier` to keep the worker's current tier (a fresh worker starts on
`deepseek`). `--tier NAME` is refused on a worker already running on a
different tier (the REPL's `--model` is baked into the launch command) —
the error points at `tt pi clear <cs>`, which respawns the REPL on a
fresh session-dir (context is lost, like a normal `clear`). For
`auto --tier NAME`, a non-matching idle worker is skipped and a fresh
worker is spawned (under cap) instead, so dispatch always lands on the
requested tier. See the per-tier prompting guides for how to structure
prompts.

## Send + wait

```sh
# Named continuation (default tier: deepseek)
TID=$(tt pi send alfa [--tier NAME] [--notify] - <<'PROMPT'
TASK: ...
FILES: ...
CHANGE: ...
SUCCESS: ...
PROMPT
)
tt pi wait "$TID"        # or: tt pi wait alfa  # latest task for that worker

# Premium tier for harder work
TID=$(tt pi send alfa --tier minimax - <<<'TASK: ...')

# Let tt choose the worker
TID=$(tt pi auto [--tier NAME] [--rm|--prefer-fresh] - <<<'TASK: ...')
tt pi wait "$TID"
```

`wait` accepts a callsign, any task id (`alfa-3`), a pool id (`pool-3`), or
`all`. Add `--json` for parsed output. Lost the id? Use `tt pi results` or
`tt pi results <id>`.

Use stdin `-` with heredocs/here-strings; do **not** use process substitution.

## Useful controls

- `tt pi wait all` — join workers that are busy right now.
- `tt pi collect` — join uncollected results across a fan-out, including tasks
  that may have finished before you waited. Add `--json` if needed.
- `tt pi steer <cs> - <<<'...'` — inject a correction into the current turn
  (run-now). `send` queues for the next turn instead.
- `tt pi logs <cs>` — read-only REPL scrollback for a slow/wedged-looking worker.
- `tt pi resume <cs>` — re-drive an `interrupted` task with context intact, then
  `tt pi wait <cs>`.
- `tt pi clear <cs>` — wipe context and respawn; use only when you want a fresh
  REPL.
- `tt pi rm <cs>` — remove a persistent worker when done.
- `tt pi update [--self|--extensions|<source>]` — run `pi update` against the
  worker's private `PI_CODING_AGENT_DIR` (the pool's installed extensions
  get updated, not the orchestrator's own pi config). Forwards all args.

## Parallelism rules

- Fan out only when `FILES` scopes are disjoint; `tt` cannot detect overlap.
- Prefer `auto --prefer-fresh` for parallel fan-out to claim distinct workers.
- Join with `wait all` for still-busy workers, or `collect` when some results may
  already be complete.
- Past the worker cap, `auto` queues on the shared pool until a worker frees up.

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
  queues behind that worker's current turn. Use a full, self-contained prompt
  contract for corrective follow-ups; persistent context is not a substitute.
- **Inspect pool:** `tt pi status`.

Tier flags (`--low`/`--medium`/`--high`/`--xhigh`/`--max`) are **rejected** — thinking
effort is fixed by the registry, not independently settable. The only accepted
explicit preset is:

- `--tier default` — `9router/cx/gpt-5.6-luna` at max effort. This is
  the only active tier, so normal dispatches should omit the flag. Provider
  catalogs are discovered dynamically, but the default model is pinned.

Omit `--tier` to keep the worker's current tier (a fresh worker starts on
`default`). `--tier NAME` is refused on a worker already running on a
different tier (the REPL's `--model` is baked into the launch command) —
the error points at `tt pi clear <cs>`, which respawns the REPL on a
fresh session-dir (context is lost, like a normal `clear`). For
`auto --tier NAME`, a non-matching idle worker is skipped and a fresh
worker is spawned (under cap) instead, so dispatch always lands on the
requested tier. See [prompting-default.md](prompting-default.md) for how to
structure prompts.

## Send + wait

```sh
# Named continuation (default model)
TID=$(tt pi send alfa [--tier NAME] [--notify] - <<'PROMPT'
TASK: ...
TARGET STATE: ...
FILES / SCOPE: ...
CHANGE: ...
DO NOT: ...
SUCCESS: ...
VERIFY: ...
PROMPT
)
tt pi wait "$TID"        # or: tt pi wait alfa  # latest task for that worker

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
- `tt pi collect --digest` — the same join as one lean line per result (id,
  status, duration, one-line summary). **Prefer this for wide fan-outs**: it
  keeps result prose out of your context; pull any full body on demand with
  `tt pi results <id>`.
- `tt peek [--lines N] <window|cs>` — read any window's current pane content
  read-only (a bare window like `dev`, or a worker callsign). Use it to see
  what a pane is doing without attaching or driving it.
- `tt pipeline run (FILE|-)` — run a declarative JSON spec (ordered fan-out and
  review stages) in one call: the daemon dispatches, joins, runs the review
  gate, and returns one digest. A review stage ends with `PIPELINE_PASS` or
  `PIPELINE_FAIL: <reason>`, and a failure retries the preceding fan-out up to
  `retries` times. Use it when a fan-out plus a verification pass would
  otherwise cost you many round-trips. Every fan-out task runs on its own
  freshly spawned worker (clean context), so scopes must be disjoint. Schema
  and a worked example: `docs/pipeline.schema.json`. The review verdict is
  evidence, not acceptance — you still own the final diff review. Pipeline
  workers persist; reclaim them with `tt pi popidle` / `tt pi rm <cs>`.
- `tt pi steer <cs> - <<<'...'` — inject a correction into the current turn
  (run-now). `send` queues for the next turn instead; use `send` for review
  remediation that needs its own tracked result and acceptance checks.
- `tt pi logs <cs>` — read-only REPL scrollback for a slow/wedged-looking worker.
- `tt pi resume <cs>` — re-drive an `interrupted` task with context intact, then
  `tt pi wait <cs>`.
- `tt pi clear <cs>` — wipe context and respawn; use only when you want a fresh
  REPL.
- `tt pi rm <cs>` — remove a persistent worker when done.
- `tt pi update [--self|--extensions|<source>]` — run `pi update` against the
  worker's private `PI_CODING_AGENT_DIR` (the pool's installed extensions
  get updated, not the orchestrator's own pi config). Forwards all args.

Every verb answers `--help` (exit 0) with a scoped synopsis — use it to
discover flags instead of guessing.

## Parallelism rules

- Fan out only when `FILES / SCOPE` scopes are disjoint; `tt` cannot detect overlap.
- Prefer `auto --prefer-fresh` for parallel fan-out to claim distinct workers.
- Join with `wait all` for still-busy workers, or `collect` when some results may
  already be complete; add `--digest` to keep the join lean.
- Past the worker cap, `auto` queues on the shared pool until a worker frees up.

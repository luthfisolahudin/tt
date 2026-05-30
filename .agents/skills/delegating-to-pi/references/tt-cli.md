# tt CLI — worker pool mechanics

Operational reference for driving pi workers via `tt pi`. Read this once you've
decided to delegate (the *when/what/which-tier* judgment lives in `SKILL.md`).

Contents:
- Pick a worker — `auto` vs by-name
- Choose flavor — ephemeral / fresh-persistent / persistent
- Send + wait — dispatch, wait, recover
- Parallelism — fan-out and joining

Each worker is a **live interactive pi REPL** in a tmux window of the
project session — the user can attach, watch, type into, and steer it by
hand at any time. The `tt` tool (`tt --help` for the full reference) is
the orchestrator's channel to them; drive workers only through `tt pi`
verbs, never by calling `pi` directly.

## Pick a worker

- **Don't care which worker?** Use `tt pi auto` — it picks an idle worker,
  spawns one if none is idle (under the cap), or queues on the shared pool if
  all are busy, echoes `using pi-<cs>`, and prints the task-id to `wait` on.
  The default for an independent task.
- **Continuing a specific worker's context?** Send to it by name —
  `tt pi send <cs>`. It lazy-spawns the worker if absent and queues behind its
  current turn if busy (run-next). Use a name when the follow-up needs context
  that worker already holds.
- `tt pi status` shows the pool when you want to look.

## Choose flavor

- **One-shot (ephemeral)**: `tt pi auto --rm` spawns a fresh worker, runs the
  task, and tears it down once done — no context leak, no cleanup. The clean
  default for an independent task.
- **Fresh but persistent**: `tt pi auto --prefer-fresh` spawns a NEW worker
  (under the cap) instead of reusing an idle one — a clean pi context *without*
  `--rm`'s teardown. Reach for it on **parallel fan-out** (claim distinct workers
  eagerly rather than piling several tasks onto one worker that just freed up) and
  whenever a reused worker's leftover context could bias the new task.
- **Persistent**: `tt pi send <cs>` (or `tt pi auto` without `--rm`) leaves the
  worker alive for a short chain of bounded follow-ups (e.g. "apply the refactor"
  → "fix the type errors it surfaced"). Each follow-up MUST restate the SUCCESS
  check; persistent workers accumulate context, so scope drift is the failure
  mode. `tt pi clear <cs>` wipes context mid-chain; `tt pi rm <cs>` removes the
  worker when the chain is done.

## Send + wait

```
# Named (continuation) — lazy-spawns, queues behind a busy turn:
tt pi send <cs> [--medium] [--notify] - <<'PROMPT'
TASK: ...
FILES: ...
CHANGE: ...
SUCCESS: ...
PROMPT
tt pi wait <cs>            # task-id optional → waits on the latest dispatch

# Or let tt pick the worker; capture the id it returns:
TID=$(tt pi auto [--medium] [--rm] - <<<'TASK: ...')
tt pi wait "$TID"          # works for a callsign id (alfa-3) or pool id (pool-3)
```

- `--medium` for safety-critical work (see triggers); omit for default `--low`.
- `wait` accepts a callsign (latest task), a bare task-id (`alfa-3`, **any** id —
  even an older one resolves), a pool id (`pool-3`), or `all` (join every busy
  worker in one report). It anchors on the task-id, so it won't false-positive on
  a stale `WORKER_DONE`. Add `--json` for a parsed envelope
  (`{id,status,summary,files_changed,notes,reason}`) instead of the raw block.
- `--notify` makes dispatch fire-and-forget: the worker pings this session when
  it finishes, so you can move on instead of blocking on `wait`.
- **Lost the task-id** (e.g. after a compaction)? `tt pi results` lists every
  recorded outcome (newest first); `tt pi results <id>` re-reads one. Nothing
  depends on having kept the id from dispatch.
- Inline prompts use `-` (stdin) with a heredoc/here-string, **not** process
  substitution.
- **Steer a running worker**: `tt pi steer <cs> - <<<'...'` injects a correction
  into its *current* turn (run-now), vs `send` which queues it for next.
- **Check on a worker without attaching**: `tt pi status` shows per-worker
  ELAPSED (how long the current turn has run) and QUEUE depth; `tt pi logs <cs>`
  dumps its REPL scrollback (read-only) so you can tell a slow-but-working turn
  from a wedged one before deciding to `steer` or `resume`.
- **Recover an interrupted worker**: if a worker shows `interrupted` (a turn
  ended without a clean `WORKER_DONE`/`BLOCKED` — e.g. someone hit Esc in its
  pane), use `tt pi resume <cs>` to re-drive that task to completion **with its
  context intact** (`interrupted → busy → done`), then `tt pi wait <cs>`. Reach
  for `tt pi clear <cs>` only when you actually want a fresh, context-wiped REPL.

## Parallelism

Workers run **in parallel only if their FILES are disjoint** — the tool can't
detect overlap; the orchestrator must. Fan out with several `tt pi send`s (or
`tt pi auto`s), then **join them in one call with `tt pi wait all`** rather than
one `wait` per worker. The cap is `min(cores-2, 26)`; past it, `auto` queues on
the shared pool for the next free worker to claim.

`wait all` joins only workers busy *at the instant it runs* — if some tasks may
have finished already (fast tasks, or a `--notify` batch), use **`tt pi collect`**
instead: it returns every result you have not collected yet (blocking on
in-flight ones) via a per-worker cursor, so a fan-out is joined completely
without you tracking each id. Add `--json` to either for parsed envelopes.

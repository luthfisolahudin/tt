# tt — status & handoff

_v0.10.0._ Read before touching `tt`. Design rationale lives in `docs/DESIGN.md`;
version history in `CHANGELOG.md`. The records/recovery effort is tracked in
`docs/PLAN-records-recovery.md` (R1 landed 0.9.0, R2 landed 0.10.0).

## Current state

- Single bash file (`~/code/tt/tt`, symlinked from `~/.local/bin/tt`) plus
  worker templates under `pi-worker/`. State lives under
  `${XDG_STATE_HOME:-$HOME/.local/state}/tt/<session>/` (override `TT_STATE_DIR`);
  worker runtime under `${XDG_DATA_HOME:-$HOME/.local/share}/tt/pi-worker`
  (override `TT_PI_WORKER_DIR`). See DESIGN "Files and external state".
- `tt up` builds only `dev`/`claude`, launches claude, attaches. The worker
  pool is lazy — no REPLs are pre-spawned; the first `tt pi send`/`auto`
  spawns the worker and waits for its readiness. `up` also stamps `TT_VERSION`
  into the session env and `$(state_dir)/version`.
- `tt pi wait` and `tt x send` wait forever by default; `--timeout N` bounds
  them. Internal health guards stay finite — notably a 20 s fast-fail on an
  unconsumed trigger.
- `tt pi send --low/--medium` switches tier at runtime via `pi.setThinkingLevel`,
  preserving pi context (no respawn).
- `tt x send` / `tt x list` / `tt x observe` provide cross-session messaging plus
  classifier-tuning diagnostics. See DESIGN.
- **Results are durable and id-addressable (0.9.0).** Every task — named and pool
  alike — records to `results/<id>.result`; `<cs>.result` is just the worker's
  latest-pointer for liveness. `tt pi wait <id>` resolves any id (older ones too);
  `tt pi results` re-reads outcomes after the fact; `tt pi collect` joins a
  fan-out via a per-worker cursor without dropping already-finished tasks;
  `--json` on `wait`/`status`/`results`/`collect` emits a stable envelope.
- **Interrupted workers recover in place (0.10.0).** `tt pi resume <cs>` (or the
  in-pane `/tt-resume`) re-drives an interrupted task to completion without a
  context wipe (`interrupted → busy → done`), via a `<cs>.resume` trigger the
  extension consumes; `tasks.jsonl` carries `notify` so resume re-honors
  `--notify`. `clear` still wipes; reset-to-idle was scoped out.

## Verified (manual)

Exercised live against throwaway `/tmp/tt-test-*` projects and the repo's own
session — what a handoff can trust without retesting:

- Cold `tt up` builds `dev`/`claude` only (no pi-* pre-spawned); re-running
  heals missing/dead standard windows and never duplicates.
- send → wait happy path (`<cs>.result` transits `running`→`done`); BLOCKED path;
  stale-WORKER_DONE rejection (terminal-position + nonce); interrupted
  quarantine; runtime tier switch; multi-turn context retention.
- lazy-spawn on first `send`/`auto`; `rm`/`popidle`; the `min(cores-2,26)` cap.
- `tt down` tears down session + state with no orphaned pi grandchildren.
- 20 s unconsumed-trigger fast-fail; `status: error` channel (the extension-side
  error writes themselves remain code-reviewed only).
- `tt x send` delivery (multiline, shell metachars, 4 KB bodies, FILE/stdin)
  against `cat` and a live orchestrator; `tt x list` / `ls --all`.
- **Records/recovery (0.9.0 + 0.10.0), verified live 2026-05-29** against the
  repo's own session: a real pi turn populating `results/<id>.result`;
  `wait --json` envelope; `tt pi results` listing; `notify` in `tasks.jsonl`;
  `tt pi collect` returning both tasks then advancing the cursor (re-collect =
  "nothing new"); and the headline `tt pi resume` recovery — a turn interrupted
  via Esc in the pane (→ `interrupted`/`status: other`) re-driven to `done` on
  the same REPL with context intact (`interrupted → busy → done`, same task id,
  nonce re-validated). `rm` wipes the worker's `results/<cs>-*` too. (The
  `--json`/parse/escape paths and cursor edges were additionally exercised
  against fabricated result files; the `status` reason hint is covered by those
  + logic — the live interrupt landed before any assistant text, so its body was
  empty and no hint was shown, which is correct.)

## Known limitations / not yet tested

- `tt down` reads a y/N confirmation from stdin — non-interactive callers must
  pipe `y`.
- `tt up`'s final attach fails harmlessly off a tty (expected headless).
- tmux-resurrect/continuum can race `tt up` and recreate a session with
  duplicate or shell-only `pi-*` windows. `tt up` heals this (dedups standard
  windows, revives dead REPLs), but keep `pi`/`claude` out of
  `@resurrect-processes` in `~/.tmux.conf` so stale REPL command lines are never
  resurrected.

## How to test

There is no harness — verify manually against a throwaway project. Use a real,
protocol-respecting task (do NOT ask the worker to "reply WORKER_DONE": that
makes it emit the marker WITHOUT the nonce footer, which is correctly rejected
as `interrupted`).

```sh
TD=$(mktemp -d /tmp/tt-test-XXXX); cd "$TD"
env -u TMUX tt up                       # builds dev/claude only; attach fails harmlessly off-tty
# lazy spawn on first send; task-id optional on wait
TID=$(tt pi send alfa - <<'P'
TASK: No code change needed — acknowledge receipt.
SUCCESS: acknowledged.
P
)
tt pi wait "$TID"                       # or: tt pi wait alfa
tt pi auto - <<<'TASK: ... ; SUCCESS: ...' ; tt pi wait all   # pick-for-me + fan-out join
STATE="${XDG_STATE_HOME:-$HOME/.local/state}/tt/$(tt name)"
tmux kill-session -t "=$(tt name)"; rm -rf "$TD" "$STATE"
```

For the queue/pool/--rm/--notify paths see CHANGELOG 0.5.0–0.8.1 (each was
verified live). Editing `pi-worker/extensions/tt-worker.ts` only takes effect on
a freshly launched REPL — respawn workers (`tt pi clear <cs>`) after changing
it. After syntax changes run `bash -n tt`. Live `pi` steps spend OpenAI Codex
quota — keep test tasks trivial.

## Worker pool

Complete (landed across 0.4.1–0.8.1; CHANGELOG has the increment history,
DESIGN the rationale and mechanics). Current behavior:

- **Lazy, no caste.** `tt up` pre-spawns nothing; workers (`alfa`…`zulu`) spawn
  on first `send`/`auto`, persist until removed, cap `min(cores-2, 26)`.
- **Dispatch.** `send <cs>` (named; run-next; lazy-spawns) · `auto` (pick idle →
  spawn → shared pool; echoes `using pi-<cs>`) · `auto --rm` (fresh ephemeral,
  reaped when idle) · `steer <cs|all>` (run-now injection) · `--notify`
  (fire-and-forget completion ping via the notify queue + lazy drainer).
- **Queues.** Per-worker `<cs>.queue/` (pinned) + shared `queue/` (stealable); an
  idle worker drains its own queue then steals from the pool (atomic-rename
  claim). `worker_state` keys `busy` off the `<cs>.busy` marker.
- **Wait.** `wait <cs|task-id|pool-id|all>`; task-id optional (defaults to the
  worker's latest); `all` joins every busy worker in one report.

Each increment was verified live against a throwaway project (see CHANGELOG).

## Known limitations / not yet tested

- 0.9.0/0.10.0 extension changes take effect only on a respawned REPL, and only
  interruptions on the new REPL are resumable — keep that in mind after an
  upgrade (`tt pi clear <cs>` to respawn an existing worker).
- `tt pi collect` has **no stuck guard** (like a pool wait) — bound a possibly-
  wedged worker with `--timeout`.
- A `pool-<seq>` wait has **no stuck guard** — a pooled task legitimately waits
  for a worker to free up, so an unclaimable pool task (e.g. all workers dead)
  hangs until `--timeout`.
- The pool steal was verified with a **single** idle worker. The atomic-rename
  claim is written to be safe under multiple workers racing the same pool file,
  but that concurrent contention has not been exercised live.
- `tt pi auto`'s pool branch (all workers busy at the cap) was exercised by
  hand-dropping a pool task; saturating the real cap to trigger it organically
  was not (would spend a lot of quota).
- Ephemeral no-pool-steal: `TT_WORKER_EPHEMERAL` is set in `start_repl`'s launch
  env (code-verified) but the `/proc` read during the live test was permission-
  blocked, so the env reaching the REPL — and thus the pump skipping pool
  steals — was not directly confirmed. The reap itself was confirmed.

## Possible next steps

- `tt pi logs <cs>` to dump a worker's REPL scrollback (also a v2 deferred item).
- Optional per-project config to auto-run the dev command.

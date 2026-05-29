# tt — status & handoff

_v0.4.0._ Read before touching `tt`. Design rationale lives in `docs/DESIGN.md`;
version history in `CHANGELOG.md`.

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

```sh
TD=$(mktemp -d /tmp/tt-test-XXXX); cd "$TD"
env -u TMUX tt up                       # attach fails harmlessly off-tty
TID=$(tt pi send alfa - <<'P'
TASK: reply WORKER_DONE
SUCCESS: done
P
)
tt pi wait alfa "$TID"
STATE="${XDG_STATE_HOME:-$HOME/.local/state}/tt/$(tt name)"
tmux kill-session -t "=$(tt name)"; rm -rf "$TD" "$STATE"
```

Editing `pi-worker/extensions/tt-worker.ts` only takes effect on a freshly
launched REPL — respawn workers (`tt pi clear <cs>`) after changing it. After
syntax changes run `bash -n tt`. Live `pi` steps spend OpenAI Codex quota — keep
test tasks trivial.

## Pool model v2 — in progress

The full successor to the v1 pool is specified in DESIGN "Pool model v2". It is
landing in increments.

**Landed (0.4.1):**

- `tt pi wait-all` (now also reachable as `tt pi wait all`); worker cap
  `min(cores-2, 26)`, NATO roster expanded to 26; `popidle` generalized.

**Landed (0.5.0) — control channel is now a per-worker queue:**

- `<cs>.queue/` replaces the single `<cs>.trigger`; the extension claims the
  next `<turn>.task` only when idle (200ms poll, never from `agent_end`).
- `tt pi send` enqueues behind a busy worker (run-next) and lazy-spawns an
  absent one; interrupted still needs `clear`.
- `tt pi steer <cs|all>` — run-now injection (`<cs>.steer`), untracked, does
  not clobber the last tracked result.
- `tt pi wait` task-id optional (defaults to latest); `all` pseudo-callsign
  joins all busy workers; untracked `id: -` result no longer pins `busy`.

**Landed (0.6.0) — shared pool queue + `tt pi auto`:**

- `tt pi auto` — reuse idle → spawn (under cap) → shared pool `queue/`; echoes
  `using pi-<cs>`, prints the task id.
- Cross-worker work-stealing: an idle worker drains its own queue then steals
  the lowest pool task (atomic-rename claim). Pool tasks are `pool-<seq>` and
  record to `queue-results/`; `tt pi wait pool-<seq>` polls that.
- `worker_state` busy now keys off the `<cs>.busy` marker (set on any turn);
  `tt pi wait` accepts a bare task-id; `tt pi status` shows a pool footer.
- **Verified live**: lazy-spawn, queue claim, two-task FIFO drain, steer
  (idle + into-turn), result not clobbered by steer, `wait` optional/bare id,
  `wait all`, `auto` worker-assign, pool steal by an idle worker, `wait pool-N`.

**Landed (0.7.0) — ephemeral workers:**

- `tt pi auto --rm` spawns a fresh ephemeral worker (non-immortal callsign),
  marked `<cs>.ephemeral` → `TT_WORKER_EPHEMERAL=1`, which makes its pump skip
  pool steals so it reliably goes idle. Reaped daemonlessly by
  `reap_ephemeral_workers` (swept by auto/status and the worker's own wait).
- **Verified live**: auto --rm spawn → run → reap (window gone, state cleaned).

**Landed (0.8.0) — lazy zero-baseline pool; immortals and `add` gone:**

- `tt up` pre-spawns nothing (`ensure_pi_repls` removed) — session is just
  `dev` + `claude`; workers lazy-spawn on first `send`/`auto`.
- Immortal caste removed (`IMMORTALS`/`is_immortal` deleted): `rm` works on any
  callsign, `popidle` pops the highest idle, `auto --rm` picks any free name.
- `tt pi add` removed entirely.
- **Verified live**: `tt up` → only dev/claude (0 pi-*); lazy-spawn on send;
  `rm alfa` (former immortal) succeeds.

**Landed (0.8.1) — `--notify`:**

- `tt pi send/auto --notify` — worker appends `<id> <status>` to `notify/` and
  spawns the lazy single-instance drainer (`tt pi notify-drain`), which
  coalesces pending pings into one paste and delivers via the shared `x_deliver`
  (refactored out of `tt x send`). Fire-and-forget: the worker never waits.
- **Verified live**: drainer coalesce/deliver/delete/idle-exit + single-instance
  against a fake orchestrator; `send --notify` end-to-end (extension writes msg,
  spawns drainer, `[tt] alfa-1 done` delivered).

**Pool model v2 is feature-complete.** Remaining: the consumer/doc
reconciliation pass (#10) and final tidy-up (#12).

## Known limitations / not yet tested (v2)

- `<cs>.result` holds only the latest tracked task. Waiting on an older task id
  whose result has been overwritten by a newer task will not resolve — wait on
  the latest (the default) or use `wait all`. (Pool tasks are immune: each
  `pool-<seq>` has its own `queue-results/` file.)
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

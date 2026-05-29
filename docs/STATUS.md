# tt — status & handoff

_v0.4.0._ Read before touching `tt`. Design rationale lives in `docs/DESIGN.md`;
version history in `CHANGELOG.md`.

## Current state

- Single bash file (`~/code/tt/tt`, symlinked from `~/.local/bin/tt`) plus
  worker templates under `pi-worker/`. State lives under
  `${XDG_STATE_HOME:-$HOME/.local/state}/tt/<session>/` (override `TT_STATE_DIR`);
  worker runtime under `${XDG_DATA_HOME:-$HOME/.local/share}/tt/pi-worker`
  (override `TT_PI_WORKER_DIR`). See DESIGN "Files and external state".
- `tt up` is non-blocking: heals windows, launches claude, fires the immortal
  REPLs asynchronously, attaches at once. REPL readiness is waited on lazily by
  the first `tt pi send`. `up` also stamps `TT_VERSION` into the session env and
  `$(state_dir)/version`.
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

- Cold `tt up` builds `dev`/`claude`/`pi-{alfa,bravo,charlie}`; re-running heals
  missing/dead windows and never duplicates.
- send → wait happy path (`<cs>.result` transits `running`→`done`); BLOCKED path;
  stale-WORKER_DONE rejection (terminal-position + nonce); interrupted
  quarantine; runtime tier switch; multi-turn context retention.
- `tt pi add`/`rm`/`popidle` and the 5-worker cap.
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
- **Verified live** against a throwaway project: lazy-spawn, queue claim,
  two-task FIFO drain, steer (idle + into-turn), result not clobbered by steer,
  `wait` with optional id, `wait all` with nothing busy.

**Still v1 / not yet built:** the **shared pool queue + cross-worker
work-stealing**, `tt pi auto` front door, lifecycle-by-`--rm`, `--notify`, the
lazy zero-baseline pool (`tt up` still pre-spawns the three immortals), and
removing the immortal caste + `tt pi add`. Immortals and `tt pi add` still
exist.

## Known limitations / not yet tested (v2)

- A `steer` to an **idle** worker starts a fresh untracked turn; while it runs,
  `tt pi status` reports the worker `idle` (untracked turns don't write a
  result). Cosmetic; the queue pump still waits for true idle before claiming.
- `<cs>.result` holds only the latest tracked task. Waiting on an older task id
  whose result has been overwritten by a newer task will not resolve — wait on
  the latest (the default) or use `wait all`.
- The shared pool queue path in the extension's claim logic (atomic rename) is
  written to be multi-worker-safe but is not yet exercised (no pool queue yet).

## Possible next steps

- `tt pi logs <cs>` to dump a worker's REPL scrollback (also a v2 deferred item).
- Optional per-project config to auto-run the dev command.

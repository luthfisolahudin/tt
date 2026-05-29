# Changelog

Notable changes to `tt`, newest first. Reconstructed from git history and prior
`docs/STATUS.md` notes. There are no release tags; versions follow the `VERSION`
constant in `tt` and the commit-message milestones (the constant jumped
0.3.0 → 0.3.4, but 0.3.1–0.3.3 were tracked as distinct milestones).

## [0.8.0] — 2026-05-29

Pool model v2, increment 5 — lazy zero-baseline pool; the immortal caste and
`tt pi add` are gone. Verified live (tt up creates only dev/claude; first send
lazy-spawns; rm of a former-immortal succeeds). This is the worker-model
simplification: one kind of worker, all lazy, all removable.

- **`tt up` pre-spawns no workers** — `ensure_pi_repls` removed; the session is
  just `dev` + `claude`. A worker's REPL is created on the first
  `tt pi send <cs>` / `tt pi auto` (lazy spawn was already in place).
- **Immortal caste removed** — `IMMORTALS`/`is_immortal` deleted; `tt pi rm`
  works on any callsign, `popidle` pops the highest idle worker, `auto --rm`
  picks any free callsign. `alfa`/`bravo`/`charlie` are now ordinary names.
- **`tt pi add` removed** — spawning is implicit via `send`/`auto`; there is no
  no-task spawn verb (a human pre-warms by sending a trivial task or running
  `pi` in a window).

## [0.7.0] — 2026-05-29

Pool model v2, increment 4 — ephemeral workers. Verified live (auto --rm spawns
a fresh worker, runs, and is reaped after wait).

- **`tt pi auto --rm (FILE|-)`** — spawn a fresh **ephemeral** worker for a
  clean one-shot, torn down once its task (and any pinned follow-ups) finish.
- Ephemeral workers (`<cs>.ephemeral` marker → `TT_WORKER_EPHEMERAL` in the
  REPL env) **never steal shared-pool work**, so they reliably reach idle.
- **Daemonless reaping**: `reap_ephemeral_workers` tears down an idle ephemeral
  worker with an empty queue; swept by `auto`/`status` and on the worker's own
  `wait`. Always picks a non-immortal callsign.

## [0.6.0] — 2026-05-29

Pool model v2, increment 3 — the shared pool queue + `tt pi auto` front door.
Verified live (auto worker-assign + lazy spawn, pool steal by an idle worker,
`wait` on a pool id, bare-task-id wait).

- **`tt pi auto [--low|--medium] (FILE|-)`** — dispatch without choosing a
  worker: reuse an idle worker → else spawn one (under the cap) → else queue on
  the **shared pool**. Echoes `using pi-<cs>` to stderr; the task-id goes to
  stdout (`TID=$(tt pi auto …)`).
- **Shared pool queue** (`queue/`) with cross-worker **work-stealing**: a task
  with no home is claimed by the first worker to go idle (atomic-rename claim,
  after its own pinned queue). Pool tasks use id `pool-<seq>` and record to a
  dedicated `queue-results/<id>.result`, so a steal never clobbers the stealing
  worker's own result. `tt pi wait pool-<seq>` polls that (no stuck guard — a
  pool task legitimately waits for capacity; bound it with `--timeout`).
- **`tt pi wait` accepts a bare task-id** (`tt pi wait alfa-3` derives the
  callsign), so `tt pi wait $(tt pi auto …)` just works.
- **`<cs>.busy` marker** — `worker_state` now keys "busy" off a marker the
  extension maintains (set on any turn: tracked, stolen-pool, or steer), not
  result parsing. Fixes the steer-into-idle status wrinkle and supports pool
  tasks that record their result elsewhere.
- **`tt pi status`** footer shows unclaimed pool tasks; it now always exits 0
  (was returning the arithmetic-false of the pool check on an empty pool).

## [0.5.0] — 2026-05-29

Pool model v2, increment 2 — a cross-cutting **control-channel** shift, so a
minor bump. The single-slot `<cs>.trigger` is gone. Verified live against a
throwaway project (lazy-spawn, queue claim, two-task FIFO drain, steer,
steer-not-clobbering-results, `wait all`).

- **Per-worker task queue.** `tt pi send` now appends `<cs>.queue/<turn>.task`;
  the `tt-worker` extension claims the lowest-numbered task — only when the
  REPL is genuinely idle — via a 200ms poll (never from inside `agent_end`,
  where a send is rejected as "already processing"). Claiming is an atomic
  rename, ready for the shared pool queue to come.
- **`send` enqueues instead of refusing.** A send to a busy worker queues
  behind the current turn (run-next) rather than erroring; a send to an absent
  worker lazily spawns it (under the cap). Interrupted workers still need
  `clear`.
- **`tt pi steer <cs|all>`** — run-now injection on a separate `<cs>.steer`
  channel, steered into the current turn (or a fresh turn if idle), bypassing
  the queue. Untracked: no task-id, no result of its own, and it never
  clobbers the last tracked task's result.
- **`tt pi wait` task-id is optional** (defaults to the worker's latest
  dispatch), and **`all` is a pseudo-callsign**: `tt pi wait all` joins every
  busy worker. `wait-all` / `steer-all` remain as aliases.
- **Worker-state fix:** an untracked turn's `id: -` result no longer pins a
  worker to `busy` forever — a terminal untracked result reads as `idle`.

## [0.4.1] — 2026-05-29

First increment of the **pool model v2** (see DESIGN). Additive, no change to the
trigger/result control channel yet.

- **`tt pi wait-all [--timeout N] [callsign...]`** — join several workers in one
  call. Blocks until each target's latest task reaches a terminal result, then
  prints one consolidated report. Bare form waits on all busy workers. This is
  the fan-out join that keeps a multi-worker dispatch O(1) in the orchestrator's
  context instead of one `wait` per worker.
- **Worker cap is now `min(cores-2, 26)`** (was a fixed 5), enforced on `add`.
  26 = NATO-letter exhaustion (`zulu`); the NATO roster is expanded to all 26.
- **`tt pi popidle`** generalized to the highest existing non-immortal worker
  (was hard-coded to `echo`/`delta`).

## [0.4.0] — 2026-05-29

- **Session version stamping.** `tt up` writes the running tt version to the
  tmux session env as `TT_VERSION` and to `$(state_dir)/version`; newly spawned
  worker REPLs also receive `TT_VERSION`.
- **Worker pi-worker split** (2026-05-28). Workers launch with
  `PI_CODING_AGENT_DIR=${XDG_DATA_HOME:-$HOME/.local/share}/tt/pi-worker`
  (override `TT_PI_WORKER_DIR`; legacy `TT_PI_AGENT_DIR` still honored) so normal
  `pi` sessions keep using `~/.pi/agent` and never load `tt-worker.ts`. The
  worker runtime dir is real and writable, lazily filled missing-only.

## [0.3.9] — 2026-05-18

- **`tt x send` waits for safe Claude Code input.** Serializes per target with
  `x-send.lock`, rejects unsafe/queued UI states, and classifies the bottom
  prompt (incl. highlighted suggestions) before pasting.
- **`tt pi wait` waits forever by default**; `--timeout N` bounds it
  (`--timeout 0` is explicit forever). Internal health guards stay finite.
- **`tt x observe`** added — passive, read-only classifier-tuning loop; events
  stored in the global `x-observe.sqlite`, deduped on payload (2026-05-26).

## [0.3.8] — 2026-05-17

- **Session discovery** — `tt x list` / `tt x ls [--all]` enumerate tt sessions
  available to message; `tt up` writes `$PWD` to `$(state_dir)/project`.
- `tt pi remove` alias for `tt pi rm`; fall back to a fresh `claude` when
  `--continue` finds no conversation.

## [0.3.7] — 2026-05-16

- **Cross-session messaging** — `tt x send <session-id> (FILE|-)` delivers a
  message into another tt session's orchestrator via tmux bracketed paste.
- Renamed `tt pi down` → `tt pi rm`; self-correcting error when a `send` source
  is not a file; `delegating-to-pi` skill recalibrated to delegate-first.

## [0.3.6] — 2026-05-16

- **`tt up` no longer black-screens during boot.** `up_cmd` runs
  `auto_launch_claude` before `ensure_pi_repls`, and `start_repl` launches pi
  under `nice -n 19` (+ `ionice -c3` where available) so the claude TUI keeps
  priority.

## [0.3.5] — 2026-05-16

- **Async REPL startup.** `tt up` is instant: it fires `start_repl` for every
  immortal and attaches at once; the 40 s readiness wait moved to `tt pi send`
  via `ensure_repl_ready` (lazy, per-target). New `starting` worker state.

## [0.3.4] — 2026-05-16

- **`tt up` is idempotent and heals** missing/dead/duplicate windows
  (`ensure_standard_windows`, `dedup_windows`, `ensure_repl`).
- Fixed: `tt down` aborting mid-teardown under `set -euo pipefail` (now SIGTERMs
  the whole pi process group); duplicate `pi-*` windows from a
  tmux-resurrect/continuum race; kill-window races.

## [0.3.3] — 2026-05-16

- **Control-channel hardening.** The trigger is consumed by rename
  (`<cs>.trigger` → `<cs>.trigger.consuming`), not read-then-truncate.
  `<cs>.result` became a lifecycle file (`running`→`done`/`blocked`/`other`/
  `error`), enabling the 20 s fast-fail on an unconsumed trigger. `clear`
  appends a `{"clear":<gen>}` marker so task ids never recur.

## [0.3.2] — 2026-05-16

- **Nonce promoted to a dedicated field** in the `WORKER_DONE` / `BLOCKED`
  blocks; the live-REPL model was comprehensively retested at this milestone.

## [0.3.1] — 2026-05-16

- **Robust task-completion detection** — random per-dispatch nonce validation,
  terminal-position check (`WORKER_DONE` must be last), and `interrupted`
  quarantine (`status=other`) guarding against stale/embedded markers.

## [0.3.0] — 2026-05-16

- **State dir moved to XDG** — `${XDG_STATE_HOME:-$HOME/.local/state}/tt/`
  (override `TT_STATE_DIR`); state survives reboots.
- **`tt up` auto-launches claude** (`claude --continue …`) when the `claude`
  pane is a bare shell.
- **Global `APPEND_SYSTEM.md` auto-injected** unless the cwd has its own
  `.pi/APPEND_SYSTEM.md`.

## [0.2.0] — 2026-05-16

- **Runtime tier switching** — `send --low/--medium` applies the tier via
  `pi.setThinkingLevel` without respawning; pi context is preserved.
- **`delegating-to-pi` made a global skill** and kept out of pi workers
  (`--no-skills` + `pi-worker/settings.json`) so a delegate can't become the
  orchestrator.

## [0.1.0] — 2026-05-16

- **Initial release.** Per-project tmux session (`dev`, `claude`,
  `pi-{alfa,bravo,charlie}` windows) plus an on-demand pi worker pool (cap 5);
  `tt up`/`down`/`attach`/`name` and
  `tt pi send`/`wait`/`status`/`clear`/`add`/`rm`/`popidle`; the
  `delegating-to-pi` consumer skill.
- **Live pi REPL model** — superseded the initial `pi -p` one-shot +
  pane-watermark prototype: each `pi-*` window now hosts a persistent
  interactive pi REPL driven through the `tt-worker.ts` extension over plain
  trigger/result files.

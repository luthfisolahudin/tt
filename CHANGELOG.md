# Changelog

Notable changes to `tt`, newest first. Versions follow the `VERSION` constant
in `tt`; each is tagged `v<x.y.z>` (annotated). Use `git diff v<x.y.z>
v<x.y.z>` to inspect a range.

## [0.14.1] — 2026-07-14

- Replaced provider/model-named tiers with one model-agnostic `default` tier,
  backed by CosmosHub Qwen 3.7 Max at max effort. Removed `minimax` and every
  `cosmos-*`, `fast`, and `deep` tier.
- Replaced the duplicated Bash/TypeScript tier mappings with one data-driven
  registry in `tt`. The worker extension now treats a task's tier as opaque
  metadata; model and effort remain fixed at REPL launch.
- Removed-tier workers are labeled `stale:<name>` and cannot receive named,
  auto-reused, or shared-pool work until `tt pi clear` respawns them on the
  current default; this prevents old live REPLs from masquerading as Qwen.
- Switched the required custom Pi `cosmoshub` provider from OpenAI Chat
  Completions compatibility to Anthropic Messages compatibility.
- Added `docs/MODEL_DECISION.md`: a dated five-model coding-agent benchmark,
  price snapshot, weighted decision rubric, and re-evaluation procedure. Qwen
  won the balanced default; image-capable Gemini models remain available
  directly through Pi/OpenCode rather than as worker tiers.

## [0.14.0] — 2026-07-12

- Added seven opt-in CosmosHub benchmark tiers for DeepSeek V4 Flash/Pro,
  GLM-5.2, Kimi K2.7 Code, MiMo V2.5/Pro, and Qwen 3.7 Max.
- Preserved `deepseek` as the default and `minimax` as the existing escalation;
  benchmark candidates do not become routing defaults before scoring.
- Added `max` to the worker extension's supported thinking levels and reject
  standalone `--max` consistently with the other tier-owned effort flags.
- Requires the custom `cosmoshub` provider in `~/.pi/agent/models.json` with
  `COSMOSHUB_API_KEY` available to worker processes.
- Added `TT_PI_ENV_VARS`, a space-separated allowlist of credentials copied
  from the calling shell into the tmux session on `tt up` and worker spawn. It
  defaults to `COSMOSHUB_API_KEY` and does not write values to tt state files.
- Documented that worker runtimes using `pi-multi-auth` must hide `cosmoshub`
  from rotation so its environment-authenticated custom provider passes through.

## [0.13.2] — 2026-07-11

- Fixed `tt pi auto` and `tt pi auto --rm` aborting under `set -u` when no
  `--tier` was supplied. Five auto-dispatch paths still referenced the removed
  `PI_DEFAULT_TIER` name instead of the canonical `PI_TIER_DEFAULT`.

## [0.13.1] — 2026-06-28

Tier-change fix found in live testing. PATCH bump: same logical change
as 0.13.0, just made correct. The 0.13.0 commit was local and never
pushed; amending it.

- `pi_send_cmd` / `pi_auto_cmd` now write the requested tier to
  `<cs>.tier` **before** any spawn work, so `start_repl` launches the
  REPL with the right `--model`. Previously, the default tier was
  written first, the REPL launched with the default model, and only
  then the tier file was overwritten — a silent wrong-model
  dispatch.
- `spawn_pi_window` no longer overwrites the tier file. Callers
  (heal path in `ensure_standard_windows`, `pi_send_cmd`,
  `pi_auto_cmd`) write the desired tier before calling it. The
  heal path was updated to write the default explicitly.
- Tier change on a running worker is now **refused** with a clear
  error pointing at `tt pi clear <cs>` (respawns the REPL; loses
  context, like a normal `clear`).
- `auto --tier NAME` skips a non-matching idle worker and spawns
  a fresh worker (under cap) so dispatch always lands on the
  requested tier. The pool branch is refused when `--tier` is
  set, since it cannot guarantee a matching worker.
- All four scenarios verified live (2026-06-28) against a
  throwaway `/tmp/tt-test-*` project: fresh-worker + `--tier
  minimax` launched the REPL with `--model
  opencode-go/minimax-m3:high` and the worker's response
  self-identified as MiniMax-M3; a tier flip on a running
  worker was refused; `auto --rm --tier minimax` spawned
  fresh on the right model; `auto --tier minimax` with an
  idle `deepseek` worker present skipped it and spawned a
  fresh `minimax` worker.

## [0.13.0] — 2026-06-28

Model tiers: a named preset that bundles (model, thinking effort). `tt pi
send` and `tt pi auto` now accept `--tier NAME`; the legacy
`--low`/`--medium`/`--high`/`--xhigh` flags are rejected with a pointer
to `--tier` (effort is fixed per tier, not independently settable).
**MINOR** because this is more than a flag: a new tier registry in
`tt`, a state-file semantic change (`.tier` stores a tier name, not
an effort), a new extension mapping (tier → effort), and per-tier
prompting reference docs. Two tiers ship, both via the `opencode-go`
provider:

- `deepseek` (default) — `opencode-go/deepseek-v4-flash` at xhigh
  effort. Cost-efficient default for high-volume, structured work.
- `minimax` — `opencode-go/minimax-m3` at high effort. Premium tier
  for harder or longer-horizon work; positioned above `deepseek`
  even at lower effort, because the model's higher base capability
  earns its way.

`<cs>.tier` now stores the tier name; `start_repl` derives
`--model $provider:$effort` from it. The `tt-worker` extension
maps tier → effort for `setThinkingLevel`. Legacy `.tier` files
containing a raw effort (`xhigh` etc.) are normalized to the
default tier on read — no manual migration, but respawn workers
(`tt pi clear <cs>`) to load the new extension.

**Sharp edge:** the model is baked into the REPL's `--model` flag
at launch. `--tier NAME` on a `tt pi send` / `auto` to a worker
already running on a different tier updates the file and the
extension's `setThinkingLevel`, but does **not** swap the model —
to pick up a new model, respawn the REPL (`tt pi clear <cs>`;
context is lost, like a normal `clear`).

Per-tier prompting reference docs added at
`skills/delegating-to-pi/references/prompting-deepseek.md` and
`prompting-minimax.md`; `prompting-and-tiers.md` links to both.

## [0.12.0] — 2026-06-17

Locked-tier runtime swap: all pi workers now run
`opencode-go/deepseek-v4-flash:xhigh`. The tier is fixed at xhigh — the
prior `--low`/`--medium`/`--high`/`--xhigh` flags on `tt pi send`/`auto` are
rejected, and the model string hard-codes `:xhigh`. `PI_DEFAULT_TIER` and
`PI_MODEL_PROVIDER` are the single source of truth; the deployment provider
choice and reasoning are recorded in `docs/DESIGN.md` (model tiers).

- `tt pi update [<args>...]` — run `pi update` against the worker's private
  `PI_CODING_AGENT_DIR` (`$TT_PI_WORKER_DIR`), so the pool's installed
  extensions get updated — not the orchestrator's own pi config. Forwards
  all args and exit code. No `tt` session required. Useful after bumping
  the bundled `pi-worker/extensions/tt-worker.ts` or after upgrading pi
  globally, when worker REPLs need a refresh.
- `delegating-to-pi` skill: prompt-clarity + self-verification rules
  consolidated into `references/prompting-and-tiers.md`; `VERIFY` bumped
  from optional to recommended, and broadened to include prompted reviews
  (not only shell commands) so a worker can be told to re-read its own
  output as a sanity check.

## [0.11.0] — 2026-06-15

`tt up` can now build a project's fixed windows from an optional
`<project>/.tt/windows.json` — split panes, pre-run per-pane commands, and swap
the orchestrator agent (some projects run `pi` or another agent in the `claude`
window, not Claude Code). Absent file → the historical `dev` + `claude` default,
via the **same code path** (one normalized config, defaults applied in jq).

- Schema: `docs/windows.schema.json`. Shape: `{ dev, claude, extra_windows[] }`
  where `dev`/`claude` are *roles* (claude is always the attach target) sharing
  one window shape `{ name?, layout?, panes:[{cmd?, enter?}] }`.
- Pane commands are applied with a **bare-shell guard**: a cmd is (re)sent only
  when its pane is a bare shell — `tt up` stays idempotent (no doubled panes, no
  injection into a running process) and a crashed `enter:true` daemon relaunches
  on the next `tt up`. `enter:false` panes pre-type once on creation and are
  never re-sent.
- Heals at **window** granularity: a missing window is recreated + laid out; a
  partially-closed multi-pane window is left alone (not re-split).
- Panes are targeted by `pane_id`, not index — safe under `pane-base-index 1`.
- Requires `jq`; without it tt falls back to the legacy `dev`+`claude` layout and
  ignores the file. Verified live against a throwaway tmux server: custom layout,
  `enter:false` buffer correctness, idempotent re-`up`, default fallback, and
  malformed-config rejection. `bash -n` clean.

## [0.10.6] — 2026-06-01

Pi workers now strip worker-only exclusion blocks from loaded context files before
each turn. Wrap orchestrator-only guidance in `AGENTS.md` / `CLAUDE.md` with
`<!-- pi-worker:exclude-start -->` and `<!-- pi-worker:exclude-end -->` to keep
that section out of the worker's system prompt while leaving normal pi/Claude
context loading untouched. The filter is implemented in the `tt-worker`
extension's `before_agent_start` hook and fails closed for an unterminated start
marker by stripping to the end of that context file. **Extension changed —
respawn workers with `tt pi clear <cs>` to load it.** Syntax/transpile and
fake-hook checks passed; no live pi turn was run.

## [0.10.5] — 2026-05-31

`tt pi send` / `tt pi auto` now accept **`--high`** and **`--xhigh`** in
addition to `--low`/`--medium`, and the worker extension records/applies those
tiers via `pi.setThinkingLevel` at task claim time. The tier still sticks per
worker until the next explicit tier flag. **Extension changed — respawn workers
with `tt pi clear <cs>` to load high/xhigh runtime switching.** Parser/syntax
paths were checked; live high/xhigh switching was not re-exercised to avoid spending
pi quota. Docs and the delegation skill now include the GPT-5.5 reasoning-budget
rule of thumb: low = one-step routine, medium = safety-critical / 2–4 step, high
= 5–8 step costly-wrong work, xhigh = rare deep branching.

## [0.10.4] — 2026-05-29

`tt pi logs [--lines N] <cs>` — dump a worker's pi REPL pane scrollback to
stdout (read-only; default 200 lines of history), so the orchestrator can tell
a slow-but-working turn from a wedged one without attaching. It does **not**
drive the worker: the long-standing "never `capture-pane`" rule is about the
control path (claiming tasks, detecting completion), not a human/orchestrator
read of the pane. Closes the last deferred observability item. Bash-only.

## [0.10.3] — 2026-05-29

Result lifecycle timestamps. The extension now stamps `started_at` into the
`running` record (when a task is claimed) and `ended_at` into the terminal
record (`agent_end`), so every durable `results/<id>.result` carries the turn's
wall-clock. `tt pi wait`/`results`/`collect --json` envelopes gain `started_at`,
`ended_at`, and a derived `duration_s` (all `null` for older records or a
still-running task), and `tt pi results` shows a `DUR` column — so you can
confirm how long a turn actually took (e.g. that a `sleep 45` task really slept),
do stall analysis, and reason about cost. **Extension changed — respawn workers
(`tt pi clear <cs>`) to load it; only turns on the new REPL get timestamps.**

## [0.10.2] — 2026-05-29

In-flight observability + dispatch ergonomics (bash-only; no extension change).
`tt pi status` now shows, for each worker, **ELAPSED** (how long the current turn
has been running, from the `<cs>.busy` marker's mtime) and **QUEUE** (`+N` pinned
tasks stacked behind a busy worker) — so a busy worker is no longer an opaque
`busy` and you can tell progress/backlog at a glance; `--json` gains `elapsed_s`
(null unless busy) and `queued`. `tt pi auto` gains two flags: **`--prefer-fresh`**
spawns a NEW worker before reusing an idle one (under the cap) — for eager
parallel fan-out and a clean pi context, since a reused idle worker still carries
its previous task's context (`--rm` still implies fresh and wins if both given);
and **`--json`** emits a single `{"worker","task_id","routed"}` line (routed =
`idle|spawn|pool|ephemeral`, the actual placement) instead of the stderr/stdout
split. `tt pi wait all` now prints a stderr hint pointing at `tt pi collect` when
a non-busy worker holds a result that finished *before* the join (so `all`
skipped it) — its semantics and exit-code contract are unchanged.

## [0.10.1] — 2026-05-29

Completion-footer robustness. The `agent_end` validator no longer demands that
every line after `WORKER_DONE`/`BLOCKED` be a `field: value` pair — it trusts the
unguessable per-task **nonce** as proof of completion and tolerates multi-line
field values and trailing prose, so a genuinely finished turn with an untidy
footer is `done`, not a false `interrupted` that wastes a `resume`. The terminal
marker is now whichever of the two appears *last* (a final `WORKER_DONE` is no
longer masked by an earlier `BLOCKED`). `tt pi wait all` gains a one-line tally on
stderr (e.g. `2 done · 1 other`; stdout stays the joined bodies) and a documented
exit-code contract. Worker contract (`APPEND_SYSTEM.md`) is unchanged — it still
asks for one clean block; only the parser is forgiving. **Extension changed —
respawn workers (`tt pi clear <cs>`) to load it.**

## [0.10.0] — 2026-05-29

In-place interrupt recovery (Release 2 of the records/recovery plan). Recover an
**interrupted** worker without a context wipe: re-drive its task to completion,
`interrupted → busy → done`. Verified live. **Extension changed — respawn
workers (`tt pi clear <cs>`) to load it; only interruptions that happen on the
new REPL are resumable.**

- **`tt pi resume <callsign>`** — re-drive an interrupted worker's task to
  completion, keeping its live REPL context (vs `clear`, which respawns on a
  fresh session-dir and loses it). Writes a `<cs>.resume` trigger the extension
  watches; the extension rehydrates the pending task's id/nonce/notify and
  re-sends the turn, so the normal `agent_end` validator closes it to `done`.
  Requires the REPL running and the worker interrupted; the turn runs async
  (join with `tt pi wait`).
- **`/tt-resume`** — the same recovery typed in the worker's own pi pane (an
  extension `registerCommand`), for when you interrupted it from there.
- **`tasks.jsonl`** rows gain a `notify` field so a resumed task re-honors the
  original `--notify`.

(Reset-to-idle — abandoning the interrupted task — was scoped out; `resume`
recovers, `clear` wipes.)

## [0.9.0] — 2026-05-29

Task records & observability (Release 1 of the records/recovery plan). Closes
the read side of the control channel:
durable, id-addressable, machine-readable results. Verified live. **Extension
result-write path changed — workers must be respawned (`tt pi clear <cs>`) to
pick it up.**

- **Unified result store.** Every task — named `<cs>-<turn>` and pool
  `pool-<seq>` alike — records to `results/<id>.result`. `<cs>.result` is demoted
  to the worker's latest-pointer (the only file `worker_state` reads); pool
  results migrate off `queue-results/`. The named and pool `wait` read paths now
  share one per-id store, so **any task-id resolves** — including an older one
  whose latest-pointer has since advanced (removes the v1 "overwritten result
  won't resolve" limitation).
- **`tt pi results [<cs>|<task-id>]`** — list every recorded outcome (newest
  first), filter to a worker, or re-read one result by id. The recover-an-id-you-
  lost path; nothing depends on having kept the id from dispatch.
- **`tt pi collect [all|<cs>]`** — cursor-based fan-out join (`<cs>.collected`):
  returns every result with turn > cursor, blocking on in-flight ones, then
  advances the cursor. Unlike `wait all` (busy-now only), it never drops a task
  that finished before you asked. `--timeout` bounds it (no stuck-guard).
- **`--json`** on `wait`/`wait all`/`status`/`results`/`collect` — a stable
  envelope `{id,status,summary,files_changed,notes,reason}` (raw `text` added for
  other/error), parsed from the result without a `jq` dependency.
- **`tt pi status`** interrupted/blocked rows now carry a one-line reason hint
  from the recorded result, so the recovery path is obvious at a glance.

## [0.8.2] — 2026-05-29

Docs/consumer reconciliation closing out pool model v2 — **no behavior change**
(`tt` edits are comments only).

- **Consumer skill** (`delegating-to-pi/SKILL.md`) rewritten to the final `tt pi`
  surface: lazy pool (no immortals/`add`), `auto`/`--rm`/`steer`/`--notify`,
  `wait <cs|task-id|pool-id|all>`, `-` stdin idiom; plus a separate-provider/
  budget framing. Stale triggering-eval query refreshed.
- **DESIGN/STATUS** de-changelogged: the per-increment "landed 0.x" ledgers
  collapse into a steady-state description (history lives here in CHANGELOG);
  the "Pool model v2 (proposed)" section is retitled design rationale and its
  plan-voice ("becomes", "when implemented") reconciled to past tense.
- Stale `trigger`-era comments in `tt` and the worker extension header updated
  to the queue model; STATUS "How to test" refreshed to the v2 surface (and to
  use a protocol-respecting task, not "reply WORKER_DONE").

## [0.8.1] — 2026-05-29

Pool model v2 — `--notify`. Verified live (drainer coalesce/deliver/delete/
idle-exit against a fake orchestrator; `send --notify` end-to-end).

- **`tt pi send --notify` / `tt pi auto --notify`** — fire-and-forget completion
  ping. On completion the worker appends `<id> <status>` to a session notify
  queue (`notify/`) and the task carries a `notify` flag (4th field of the queue
  task line).
- **Lazy single drainer** (`tt pi notify-drain <session>`, internal): the worker
  spawns it detached; it is single-instance (stale-pid-aware lock), coalesces all
  pending notifications into ONE paste, delivers via the shared `x_deliver`
  (the `tt x send` safe-input path, extracted for reuse), deletes delivered
  messages, and idle-exits. The worker never waits on delivery, so it goes idle /
  claims the next task immediately; the drainer (own process group) survives an
  ephemeral worker's reap.

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

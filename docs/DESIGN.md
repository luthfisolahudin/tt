# tt — design & rationale

## Why it exists

The orchestrator (Claude Code) delegates mechanical subtasks to **pi**, a
code worker wired to OpenAI Codex (ChatGPT Plus, flat-rate). Running
`pi -p "..."` directly in Bash had two problems:

1. **No visibility / no control** — the user could not watch pi work, let
   alone stop or steer it mid-task.
2. **No parallelism** — every call was ephemeral and serial.

`tt` solves both by giving each project one tmux session that hosts the
dev server, the orchestrator, and a pool of pi workers. One place to
attach; one place to see everything.

## Session model

- **One tmux session per project**, named `<basename($PWD)>-<sha1($PWD)[:4]>`.
  Deterministic from the project path; the 4-char hash disambiguates
  same-named directories.
- **Standard windows**, created at session-up time (idempotent):

  | Idx | Name | Contents |
  |-----|------|----------|
  | 0 | `dev` | Empty shell in `$PWD`. Run the dev server here. |
  | 1 | `claude` | Empty shell. Launch the orchestrator here. |
  | tail | `pi-<cs>` | Live pi REPL, **created on demand** (see below). |
  | tail | user windows | Ad-hoc, created with `Ctrl-b c`. Not managed by tt. |

- **The pool is lazy: `tt up` pre-spawns no workers.** A `pi-<cs>` window and
  its REPL are created on the first `tt pi send <cs>` / `tt pi auto`, up to the
  cap (`min(cores-2, 26)`). Callsigns are NATO (`alfa`…`zulu`); none is
  special, immortal, or un-removable.
- Attach lands on `claude`.

## Pi windows host a LIVE pi REPL

The central design decision. Each `pi-*` window runs a genuine
interactive pi REPL (`pi --session-dir <dir> --model …`), launched via
`tmux respawn-pane -k` with a `; exec bash` tail so the pane survives if
pi ever exits. Rationale:

- **Visible, steerable.** The user can attach to any `pi-*` window and
  watch the turn stream, type a message, hit Esc to interrupt — exactly
  as if they had run `pi` themselves. tt's control channel and the
  human's keystrokes coexist on the same REPL.
- **No pane scraping.** An earlier design ran `pi -p` in a shell and
  recovered output by `capture-pane` + a line "watermark". That was the
  source of every hard bug tt ever had (blank-padding miscounts,
  scrollback roll-past, launch-detection races). It is gone.
- **Names are pinned.** `spawn_pi_window` sets `automatic-rename off` on
  each pi window immediately after creation so tmux cannot rename the
  window away from its `pi-<callsign>` name. All subsequent tmux calls
  target windows by name; a rename would cause "can't find window" errors.

### Lazy spawn + readiness

`tt up` builds only `dev`/`claude` and launches the orchestrator — it spawns no
REPLs. A worker boots on first use: `tt pi send`/`auto` calls `ensure_repl_ready`,
which spawns the window+REPL if absent (or restarts a dead one), stamps
`<cs>.starting`, then blocks on *that* worker's `<cs>.ready` (40 s deadline)
before enqueuing — so the startup race stays closed. `start_repl` launches pi
under `nice -n 19` (and `ionice -c3` where available) so the interactive claude
TUI keeps scheduler priority; pi workers are API-I/O bound, so the low priority
costs them little.

## The tt-worker extension — control channel

tt talks to each REPL through **`pi-worker/extensions/tt-worker.ts`**, a pi extension
auto-discovered from tt's private pi worker dir (`~/.local/share/tt/pi-worker`,
passed as `PI_CODING_AGENT_DIR`). Normal user pi sessions continue using
`~/.pi/agent` and do not load the worker extension. The extension is still
inert unless `TT_WORKER_CS` is set; tt sets that env var (and
`TT_WORKER_STATE`) only for the workers it spawns.

The extension and tt exchange plain files under
`${XDG_STATE_HOME:-$HOME/.local/state}/tt/<session>/`, all in a trivial line
format so the bash side needs no JSON parser:

- **`<cs>.queue/`** — a per-worker task queue (directory). `tt pi send`
  always **appends** a `<turn>.task` file (atomic `mv`): line 1 is
  `<task id> <tier> <nonce>`, the rest is the prompt body. The extension
  **claims** the lowest-numbered task — but only when the REPL is genuinely
  idle (`ctx.isIdle()`) — by renaming it to `<file>.claiming`, reading that
  private path, and deleting it (so a concurrent tt write is never clobbered).
  It then writes a `running` result, applies the tier (`pi.setThinkingLevel`),
  and sends the body as a fresh user turn (`pi.sendUserMessage`). Claiming is
  driven by a 200 ms poll plus a claim at `session_start`; it is **never** done
  from inside `agent_end` (the agent is still "processing" there, so a send
  would be rejected — the bug that the idle-gated poll avoids). A busy worker
  simply leaves later tasks queued until its turn ends: `send` = run-next.
- **`queue/`** — the **shared pool** (session-level, one dir for all workers).
  `tt pi auto` drops a `<seq>.task` here when no worker is free. Drain priority:
  an idle worker claims its own `<cs>.queue/` first, then **steals** the
  lowest pool task. The atomic-rename claim is the concurrency primitive —
  many workers race on the same file and only one rename wins. Pool tasks use
  id `pool-<seq>` and write their result to the unified **`results/<id>.result`**
  store (not the stealing worker's `<cs>.result`), so a steal never clobbers a
  worker's own latest-pointer and `tt pi wait pool-<seq>` polls one known path.
- **`<cs>.busy`** — a marker the extension sets while a turn is in flight (a
  tracked task, a stolen pool task, or a steer) and clears on `agent_end`.
  `worker_state` reads it to decide `busy`, because a pool task records its
  result elsewhere and a steer records none — the marker is the only reliable
  "is this REPL working" signal.
- **`notify/`** — the completion-ping queue for `--notify` tasks. On `agent_end`
  the extension appends `<id> <status>` here and spawns the **drainer** (`tt pi
  notify-drain`, detached, own process group so it outlives an ephemeral reap).
  The drainer is single-instance (`notify-drain.lock`, stale-pid-aware),
  coalesces all pending messages into ONE paste, delivers via `x_deliver` (the
  shared `tt x send` safe-input path), deletes delivered messages, and
  idle-exits. The worker never waits on delivery — it goes idle and claims the
  next task at once. This is the daemonless-work-queue pattern again, applied to
  delivery.
- **`<cs>.steer`** — run-now injection, separate from the queue. tt writes it
  (atomic `mv`); the extension consumes it by rename and sends the text
  **steered into the current turn**, or as a fresh turn if idle. It does not
  touch the pending task id, so the in-flight task still validates its own
  completion. This is `tt pi steer` / `tt pi steer all`.
- **`<cs>.resume`** — recovery trigger; presence is the whole signal (no
  payload). tt writes it (atomic `mv`); the extension consumes it by rename and
  **re-drives the worker's interrupted task to completion** — it rehydrates the
  pending id/nonce/notify (looked up from `tasks.jsonl`), writes `running`, and
  re-sends the turn, so the normal `agent_end` validator closes it. Indirection
  through the extension is required: bash cannot restore the extension's
  in-memory pending id, so a plain steer would run untracked and never reach
  `done`. This is `tt pi resume` (and the in-pane `/tt-resume` command, which
  calls the same routine directly).
- **`results/<id>.result`** — the unified id-keyed result store. The extension
  writes a lifecycle file atomically for **every** task (named `<cs>-<turn>` and
  pool `pool-<seq>` alike):
  ```
  id: <task id>
  status: running|done|blocked|other|error
  started_at: <epoch>        (ended_at: <epoch> once terminal)
  ---
  <text>
  ```
  `running` is written when a task is claimed (empty text, `started_at` stamped);
  `done`/`blocked`/`other` on `agent_end` (adds `ended_at`); `error` when the
  extension catches an internal exception (text = the message). The two
  timestamps are surfaced in the `--json` envelope as `started_at`/`ended_at`
  plus a derived `duration_s` (all `null` for older records), and as the `DUR`
  column of `tt pi results`. Because results are id-keyed and never overwritten
  by a later task, `tt pi wait <id>` resolves any task — even an older one — and
  `tt pi results <id>` re-reads it long after.
- **`<cs>.result`** — a **latest-pointer**: the extension mirrors a worker's own
  assigned-task result here (a copy of its newest `results/<id>.result`). This is
  the only result file `worker_state` reads, for liveness/idle classification.
  Pool tasks are **not** this worker's, so they are never mirrored. **Untracked
  turns** (a steered message, or a human typing into the REPL) carry no pending
  id and **do not write a result at all** — so they never clobber the last
  tracked task's latest-pointer.
- **`<cs>.ready`** — the extension touches it once the queue pump + steer
  watch are live, so `launch_repl` knows when it is safe to enqueue.
- **`<cs>.log`** — append-only, timestamped diagnostics for failures
  that have no result to attach to (watch setup, task read, pump).

## Task IDs & completion

1. `tt pi send` assigns the task id `<callsign>-<turn>` (turn = line
   count of `tasks.jsonl` + 1), appends `<cs>.queue/<turn>.task`, and appends
   `{turn,id,sent_at,tier,nonce,notify}` to `tasks.jsonl` (the `notify` flag lets
   `tt pi resume` re-honor the original `--notify`).
2. `tt pi wait <cs> [task-id]` polls `results/<task-id>.result` until its `id`
   field matches; the task-id is **optional** and defaults to the worker's latest
   dispatch. Reading the per-id store (not the `<cs>.result` latest-pointer) means
   an **older** task-id still resolves. It waits forever by default; `--timeout N` bounds
   the wait, and `--timeout 0` is explicit forever. `status` of `done`/`blocked`
   prints the assistant text and exits 0; `other` (pi answered without a valid
   marker) and `error` (extension exception) exit 1; `running` keeps polling.
   Whichever of `WORKER_DONE`/`BLOCKED` appears **last** is the terminal marker,
   so a final `WORKER_DONE` is never masked by an earlier `BLOCKED` wrapper (or
   vice-versa). `tt pi wait all` joins every busy worker (consolidated report).
3. Stuck guard: if this task's `<cs>.queue/<turn>.task` is still present while
   the worker is **not running anything** for 20 s (a dead pump/watch), `wait`
   fails fast — even on an infinite wait. If the worker is running an earlier
   queued task, the wait is legitimate and the timer is held off.

## Send → wait flow

```
orchestrator
  tt pi send <cs> <prompt-file>
    → appends <cs>.queue/<turn>.task  (line 1: <id> <tier> <nonce>; rest: body)
    → appends to <cs>.tasks.jsonl
    → prints task-id          (busy worker? the task just queues — run-next)

tt-worker.ts  (inside the pi REPL)
  200ms poll / session_start — pump(), only when ctx.isIdle():
    → claims lowest <turn>.task by rename → reads → deletes
    → writes <cs>.result  (status: running)
    → applies tier via pi.setThinkingLevel; stores nonce
    → sends body as a fresh user turn (pi.sendUserMessage)

  on agent_end:
    → tracked task: locates the last WORKER_DONE/BLOCKED marker; a matching
      `nonce:` at/after it → done/blocked (the unguessable per-task nonce is the
      proof, so multi-line field values and trailing prose are tolerated). Writes
      <cs>.result (id / status / text); goes idle
    → untracked turn (steer/human): no result write
    → next idle poll claims the next queued task

orchestrator
  tt pi wait <cs> [task-id]        (task-id defaults to latest; `all` = all busy)
    → polls <cs>.result until id matches task-id
    → waits forever by default; --timeout N bounds the wait
    → exits 0 on done/blocked; exits 1 on other/error/timeout
    → fast-fails if the task stays queued 20s while the worker is idle
```

## Worker state detection

State is derived from the window plus the control files:

- `missing` — the window does not exist.
- `starting` — the REPL is still within its boot window: either no pi
  process is alive yet but `<cs>.starting` (stamped by `start_repl`) is
  recent (< 45 s), or the process is up but `<cs>.ready` has not been
  written. `tt up` starts the REPLs asynchronously, so `tt pi status`
  run right after will show this until each worker settles.
- `down` — window exists but no pi process is alive for it (matched by
  the worker's unique `--session-dir` path via `pgrep -f`; tmux's
  `pane_current_command` is unreliable because pi runs as a grandchild)
  and it is past the boot window.
- `busy` — the `<cs>.busy` marker is set: the REPL is processing something (a
  tracked task, a stolen pool task, or a steer). The marker is the signal,
  not the result file — a pool task is not mirrored to `<cs>.result` and a steer
  records nothing, so result-parsing alone would miss them.
- `blocked` — not busy, and the worker's own last assigned-task result
  (the `<cs>.result` latest-pointer) is `blocked`.
- `interrupted` — not busy, and that result is `other` (no valid completion
  marker) or `error` (extension exception). A fresh `send` refuses on an
  interrupted worker until it is recovered. `tt pi status` surfaces a one-line
  reason from the recorded result so the recovery path is legible. Recover with
  **`tt pi resume`** (re-drive to completion, context preserved) or `tt pi clear`
  (wipe + fresh REPL). Resume restores the pending id/nonce, so the lifecycle is
  `interrupted → busy → done` on the same live REPL — see `<cs>.resume` above.
- `idle` — anything else (incl. an idle worker whose `<cs>.result` describes a
  pool/steer turn or an older task).

## Model tiers

Default tier is `low`; `--medium` on `send` is for safety-critical work.
Reasoning effort is a **runtime knob**: `send` writes the tier into the
queued task and the `tt-worker` extension applies it with
`pi.setThinkingLevel` before the turn. A tier change therefore does
**not** respawn the REPL — pi context is preserved across it. The tier
sticks (remembered in `<callsign>.tier`) until the next explicit
`--low`/`--medium`.

## Context reset

`tt pi clear` bumps `<callsign>.gen` and respawns the REPL on a new
`--session-dir` (`pi-sessions/<cs>/g<N>/`). A fresh session-dir is a
fresh pi session — no `--continue`, no leftover context. `clear`
appends a `{"clear":<gen>}` marker line to `tasks.jsonl` rather than
truncating it, so the turn counter stays monotonic and task ids
(`<cs>-<turn>`) never recur across generations.

## State files

Under `${XDG_STATE_HOME:-$HOME/.local/state}/tt/<session>/`:

| File | Contents |
|------|----------|
| `<cs>.tasks.jsonl` | One JSON line per turn: `{turn,id,sent_at,tier,nonce,notify}`; plus a `{"clear":<gen>}` marker line per `clear`. |
| `<cs>.tier` | Current pi thinking tier. |
| `<cs>.gen` | Current context generation (bumped by `clear`). |
| `<cs>.in.<N>.txt` | Prompt body for turn N. |
| `<cs>.queue/<turn>.task` | Queued task handed to the REPL (id line + body); claimed by the extension when idle. `<turn>.task.claiming.<cs>` is the transient mid-claim rename. |
| `queue/<seq>.task` | Shared pool task from `tt pi auto` (id `pool-<seq>`); any idle worker steals it after draining its own queue. |
| `results/<id>.result` | Unified id-keyed result store for **every** task (named + pool). Durable, never overwritten by a later task; read by `wait`/`collect`/`results`. |
| `<cs>.collected` | `tt pi collect` cursor: the highest turn whose result has been collected for this worker. |
| `pool.seq` | Monotonic counter for pool task ids. |
| `notify/<ts>-<pid>-<n>.msg` | `--notify` completion ping (`<id> <status>`), drained to the orchestrator. |
| `notify-drain.lock` | Single-instance lock for the notify drainer (holds its pid). |
| `<cs>.ephemeral` | Marker: this worker was spawned by `tt pi auto --rm`; it never steals pool work and is reaped once idle with an empty queue. |
| `<cs>.steer` | Run-now injection for `tt pi steer`; consumed by the extension (`<cs>.steer.consuming` is the transient mid-consume rename). |
| `<cs>.resume` | Recovery trigger for `tt pi resume` (presence = signal); consumed by the extension, which re-drives the interrupted task to completion. |
| `<cs>.result` | Latest-pointer: a copy of this worker's newest `results/<id>.result`, read by `worker_state` for liveness/idle classification. |
| `<cs>.busy` | Marker: the REPL is processing a turn (drives `worker_state` busy). |
| `<cs>.starting` | Boot stamp (`date +%s`) written by `start_repl`; marks the REPL's async boot window for `repl_starting`. |
| `<cs>.ready` | Marker: the REPL's queue pump + steer watch are live. |
| `<cs>.log` | Append-only extension diagnostics for failures with no result. |
| `pi-sessions/<cs>/g<N>/` | pi `--session-dir` for generation N. |

## What does NOT change vs the old `pi -p` flow

- `pi-worker/APPEND_SYSTEM.md` — pi's Worker Mode rules, injected by tt unless the cwd has its own `.pi/APPEND_SYSTEM.md`.
- The `TASK / FILES / CHANGE / [CONTEXT] / SUCCESS` prompt format.
- The `WORKER_DONE` / `BLOCKED:` completion markers.
- The model ladder (`gpt-5.5:low` default, `:medium` for safety-critical).
- The `tt pi send` / `wait` interface — same verbs, same task-ids.

## Cross-session messaging — `tt x send`

`tt x send [--timeout N] <session-id> (FILE|-)` pushes a message into another
tt session's orchestrator and submits it once that Claude Code TUI can safely
accept input.

Unlike pi workers, the orchestrator is a live Claude Code TUI with no
file control channel, so delivery uses tmux directly. `tt x send`
serializes per target with `<target-state>/x-send.lock`, then waits for a safe
input state: it rejects in-flight/interrupt states and a non-empty `❯` draft,
and treats an empty prompt, dim suggestion text, and queued-message banners as
safe (a fresh paste joins Claude Code's input queue or replaces its
suggestion). The exact ANSI heuristics live in the code; `tt x observe` exists
to tune them. The wait is infinite by default; `--timeout N` fails after N
seconds.

Once ready, the message (prefixed with an `[tt x from <sender>]` header) is
loaded into a per-process tmux buffer (`tt-x-$$`) and pasted with
`paste-buffer -p` (bracketed paste, so embedded newlines don't submit early),
then submitted with one `send-keys Enter` — the same primitive
`auto_launch_claude` uses.

`<session-id>` is the exact tmux session name (`tt name` in the other project).
`tt x send` refuses if the session is missing, has no `claude` window, or its
orchestrator pane is a bare shell.

### Cross-session observation — `tt x observe`

`tt x observe [run] [--interval N] [--duration N] [--all]` is a passive,
read-only diagnostics loop for tuning the `tt x send` classifier (bare
`tt x observe` aliases `run`). It samples every running tt session's `claude`
pane with the same classifier as `tt x send` and writes rows to the global
`${XDG_STATE_HOME:-$HOME/.local/state}/tt/x-observe.sqlite`, deduping on a
payload key that ignores the `ts` field. It never takes `x-send.lock`, pastes,
or sends keys — but it does log pane text, so it prints a startup warning.
`--duration 0` runs until Ctrl-C; `--all` also samples down/no-orchestrator
sessions. `scripts/import-x-observe-jsonl.sh` imports the legacy JSONL log.

## Pool model — design rationale

> This section is the **why**. The sections above describe the live mechanics;
> the CHANGELOG has the increment-by-increment history of how the model was
> built. The model below is fully implemented.

### Motivation

Benchmarked against Claude Code's `Workflow` tool, v1's agent-side cost is
**per-task coordination** (a `send` + `wait` round-trip per worker clogs the
orchestrator's own context) plus the rigidity of the immortal caste and fixed
cap. v2 removes that toil while keeping tt's structural moats — durable,
steerable, **provider-heterogeneous** workers — which an ephemeral same-budget
Workflow subagent structurally cannot match.

### One worker kind, lifecycle set by `--rm`

- No immortal caste. `alfa`/`bravo`/`charlie` are just the conventional names of
  the first lazily-spawned workers; none is un-rm-able.
- Persistence is a property of the **task**, not the worker. A worker run
  without `--rm` persists — stays named, holds context, can be continued,
  steered, and attached to. `--rm` destroys it on completion (ephemeral
  one-shot — tt's answer to a Workflow agent, on any provider).
- `tt up` spawns **zero** pi workers (`dev` + `claude` only). Workers
  materialize on first dispatch (`send`/`auto`). Baseline N=0; there is no
  no-task spawn verb — a human wanting a bare REPL sends a trivial task or runs
  `pi` in a window by hand.

### Front door: `tt pi auto`

`tt pi auto [--rm] [--notify] <prompt>` picks the worker and **echoes which one**
("using pi-alfa …") — that string is the return contract, so a later
`wait`/`steer`/follow-up can target it. Policy: reuse an idle persistent worker
→ else spawn (under cap) → else queue. `--rm` forces a fresh ephemeral worker.
It is removed when its current job is done **and its own per-worker queue is
empty** — it drains pinned follow-ups (`send <cs>` continuations that need its
context) first, but does **not** linger to steal shared-pool work: pending pool
tasks trigger the rm and a fresh re-spawn rather than keeping the ephemeral
worker alive.

### Named dispatch stays explicit — for continuation

`tt pi send <cs>` is for continuation: the task is pinned to a worker that holds
context. Now **lazy** (spawns the worker if absent) and **enqueues** when the
worker is busy — it no longer steers. `send` (next) and `steer` (now) thus get
clean, separate semantics.

### Two queues, one work-stealing drain

The distinction is **pinned vs stealable**:

- **Per-worker queue** (`<cs>.queue/`) — fed by named `send` to a busy worker.
  Pinned for context-continuity; never stolen by another worker (only the named
  worker has the context).
- **Shared pool queue** (`queue/`) — fed by `auto` when all workers are busy at
  cap. Stealable, for throughput.
- **Drain priority:** an idle worker claims its own queue first, then steals
  from the pool. Claim = atomic rename — the one concurrency primitive used
  throughout (queue claim, pool steal, lock acquire), so still **no daemon**.

### Three injection semantics

| Verb | Timing | Pinned |
|------|--------|--------|
| `steer <cs>` / `steer-all` | now, interrupts the current turn | yes |
| `send <cs>` (worker busy) | next, after the current turn | yes |
| `auto` (all busy at cap) | whenever any worker frees | no |

### Join + notify

- `tt pi wait all` — block until every busy worker reaches a terminal result;
  one consolidated report. The main fix for v1's coordination context-cost:
  one round-trip joins a fan-out instead of one `wait` per worker. (Distinct-task
  fan-out is just batched `send`s in one shell call.)
- `tt pi collect` — the join that survives timing. `wait all` targets only
  workers busy *at the instant it runs*, so a task that finished before the join
  is silently missed and the orchestrator must have kept its id. `collect`
  tracks a per-worker cursor over the durable result store and returns every
  uncollected result (blocking on in-flight ones), so a fan-out is joined
  completely without hand-tracking ids — the bookkeeping the pool should absorb.
- `--notify` (on `send`/`auto`) — fire-and-forget completion ping. The worker
  appends to a notify queue and a lazy single drainer delivers a coalesced line
  into the `claude` pane via the `tt x send` safe-input path. Built from parts
  tt already owns; the on-disk queue survives a reboot, which an in-memory
  Workflow fan-out cannot.

### Cap

`min(cores-2, 26)` — 26 = NATO-letter exhaustion (`zulu`), and a **hard ceiling
for every path, manual and auto alike**. At cap, `auto` queues rather than
spawning and any explicit spawn is refused (it may warn as it approaches). The
ceiling is the runaway backstop that makes auto-spawn safe.

### Verbs removed / deferred

- `tt pi add` is **removed entirely** — spawning is implicit via `send`/`auto`
  and lazy spawn covers every case, so there is no spawn-only verb (a human
  wanting a bare REPL opens a window and runs pi, or sends a trivial task).
  `rm` (destroy) and `clear` (reset context) stay.
- Landed since: the JSON result envelope (`--json` on `wait`/`status`/
  `results`/`collect`), the durable per-id result store, `tt pi results` /
  `tt pi collect`, in-place interrupt recovery without a context wipe
  (`tt pi resume` / `/tt-resume`), and the observability layer — `tt pi status`
  ELAPSED/QUEUE, result `duration_s`, and `tt pi logs` (read-only scrollback).
  See CHANGELOG for when.
- Deferred until needed: reset-to-idle of an interrupted task (scoped out —
  `resume` recovers it, `clear` wipes it).

### Framing — the moat

It is **provider heterogeneity**, not flat-rate Codex: any orchestrator driving
a pool of any-provider workers (pi on Codex, Kimi, Deepseek, …), each on its own
account, quota, and model. Workflow's agents are Claude subagents on the
orchestrator's own budget and cannot follow there. The consumer
`delegating-to-pi` skill states this (delegating offloads off the orchestrator's
own budget) so the orchestrator reaches for tt on heavy fan-out. (`tt x send`
and the skill are intended to generalize to non-Claude orchestrators — the tmux
substrate and file protocol are already provider-agnostic.)

## Out of scope (deliberately)

- Auto-starting the dev server (`dev` window stays an empty shell).
- Per-project `tt` config (custom dev command / default tier).

## Files and external state

| Location | Purpose |
|----------|---------|
| `~/code/tt/tt` | The tool itself (symlinked from `~/.local/bin/tt`). |
| `~/code/tt/pi-worker/` | Repo-owned worker templates: tracked `settings.json`, `APPEND_SYSTEM.md`, and `extensions/tt-worker.ts`. |
| `~/.local/share/tt/` | XDG data dir: writable runtime worker data plus symlinks to repo-owned source files. |
| `~/.local/state/tt/<session>/` | XDG state dir: trigger/result/task files per worker, plus `project` and `version` session metadata. Override with `TT_STATE_DIR`. |
| `~/.local/share/tt/pi-worker` | Real writable runtime dir passed to worker REPLs as `PI_CODING_AGENT_DIR`; override with `TT_PI_WORKER_DIR` (legacy `TT_PI_AGENT_DIR` is still honored). Lazily filled missing-only: copied `settings.json`, managed symlinks to repo-owned files such as `APPEND_SYSTEM.md` and `extensions/tt-worker.ts`, symlinked global `auth.json`/`models.json` when present, pi-owned mutable files, and `.tt-version` metadata for template-drift warnings. Existing runtime files are left alone for customization. |
| `~/.pi/agent/settings.json` | User-owned normal pi settings; tt no longer installs worker resources here. |
| `pi-worker/APPEND_SYSTEM.md` | Worker protocol injected into every pi REPL. A cwd-local `.pi/APPEND_SYSTEM.md` still takes precedence if present. |
| `.agents/skills/delegating-to-pi/` | Consumer-facing skill telling the orchestrator how to use `tt pi send`/`wait`. |

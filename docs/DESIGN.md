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
  | 2 | `pi-alfa` | Live pi REPL. **Immortal.** |
  | 3 | `pi-bravo` | Live pi REPL. **Immortal.** |
  | 4 | `pi-charlie` | Live pi REPL. **Immortal.** |
  | 5+ | `pi-delta` / `pi-echo` | Optional, on-demand. Hard cap of 5 pi. |
  | tail | user windows | Ad-hoc, created with `Ctrl-b c`. Not managed by tt. |

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

### `tt up` starts the REPLs asynchronously

`tt up` does not block on REPL readiness: it creates/heals windows, launches
the orchestrator, fires `start_repl` for every immortal (each just
`respawn-pane`s the pane and stamps `<cs>.starting`), and attaches at once. The
REPLs finish booting in the background.

**Order matters: claude first, then the pi REPLs.** `up_cmd` runs
`auto_launch_claude` before `ensure_pi_repls` so claude's alternate-screen TUI
gets a clean first paint instead of sitting black behind three concurrent pi
`node` startups; `start_repl` further launches pi under `nice -n 19` (and
`ionice -c3` where available) so the interactive TUI keeps priority. pi workers
are API-I/O bound, so the low priority costs them little.

The 40 s readiness wait is **lazy** — `tt pi send` calls `ensure_repl_ready`,
which blocks on *that* worker's `<cs>.ready` only when a task is dispatched.
The trigger is still written only after `<cs>.ready`, so the startup trigger
race stays closed; a send to a genuinely-dead worker restarts it first.

## The tt-worker extension — control channel

tt talks to each REPL through **`pi-worker/extensions/tt-worker.ts`**, a pi extension
auto-discovered from tt's private pi worker dir (`~/.local/share/tt/pi-worker`,
passed as `PI_CODING_AGENT_DIR`). Normal user pi sessions continue using
`~/.pi/agent` and do not load the worker extension. The extension is still
inert unless `TT_WORKER_CS` is set; tt sets that env var (and
`TT_WORKER_STATE`) only for the workers it spawns.

The extension and tt exchange two plain files under
`${XDG_STATE_HOME:-$HOME/.local/state}/tt/<session>/`, both in a trivial line format so the bash side
needs no JSON parser:

- **`<cs>.trigger`** — tt writes it (atomic `mv`): line 1 is
  `<task id> <tier> <nonce>`, the rest is the prompt body. The
  extension's `fs.watchFile` fires; it **consumes the trigger by
  renaming it** to `<cs>.trigger.consuming`, reads that private path,
  and deletes it — so a concurrent tt write is never clobbered by a
  truncate. It then writes a `running` result, applies the tier
  (`pi.setThinkingLevel`), and sends the body to the REPL as a user
  message (`pi.sendUserMessage`, steered if pi is mid-turn).
- **`<cs>.result`** — a lifecycle file the extension writes atomically:
  ```
  id: <task id | ->
  status: running|done|blocked|other|error
  ---
  <text>
  ```
  `running` is written the instant a trigger is consumed (empty text);
  `done`/`blocked`/`other` on `agent_end`; `error` when the extension
  catches an internal exception (text = the message). `id` is the
  trigger's id for a tt-injected turn, or `-` for a human-typed one — so
  a person typing into the REPL never confuses tt's `wait`.
- **`<cs>.ready`** — the extension touches it once its trigger watch is
  live, so `launch_repl` knows when it is safe to write a trigger.
- **`<cs>.log`** — append-only, timestamped diagnostics for failures
  that have no result to attach to (watch setup, trigger read).

## Task IDs & completion

1. `tt pi send` assigns the task id `<callsign>-<turn>` (turn = line
   count of `tasks.jsonl` + 1), writes the trigger, and appends
   `{turn,id,sent_at,tier}` to `tasks.jsonl`.
2. `tt pi wait <cs> <task-id>` polls `<cs>.result` until its `id` field
   equals the task-id. It waits forever by default; `--timeout N` bounds
   the top-level completion wait, and `--timeout 0` is explicit forever.
   `status` of `done`/`blocked` prints the assistant text and exits 0;
   `other` (pi answered without a marker) and `error` (extension
   exception) exit 1; `running` keeps polling. `BLOCKED` is classified
   ahead of `WORKER_DONE` so a real block is never masked by a trailing
   wrapper.
3. If the trigger is still sitting unconsumed 20 s after dispatch (the
   worker is wedged or its watch is dead), `wait` fails fast with a
   diagnostic even when the top-level wait is infinite.

## Send → wait flow

```
orchestrator
  tt pi send <cs> <prompt-file>
    → writes <cs>.trigger  (line 1: <id> <tier> <nonce>; rest: prompt body)
    → appends to <cs>.tasks.jsonl
    → prints task-id

tt-worker.ts  (inside the pi REPL)
  fs.watchFile fires on <cs>.trigger
    → renames <cs>.trigger → <cs>.trigger.consuming, reads, deletes it
    → writes <cs>.result  (status: running)
    → applies tier via pi.setThinkingLevel
    → stores nonce for completion validation
    → sends prompt body as a user message (pi.sendUserMessage)

  on agent_end:
    → validates: WORKER_DONE at terminal position AND nonce matches
    → writes <cs>.result  (id / status / text)

orchestrator
  tt pi wait <cs> <task-id>
    → polls <cs>.result until id matches task-id
    → waits forever by default; --timeout N bounds completion wait
    → exits 0 on done/blocked; exits 1 on other/error/timeout
    → fast-fails if the trigger is unconsumed 20s after dispatch
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
- `busy` — the last task id in `tasks.jsonl` has no matching terminal
  result yet, or the matching result's status is `running`.
- `blocked` — the last result's status is `blocked`.
- `interrupted` — the last result's status is `other` (no valid
  completion marker) or `error` (extension exception). Requires
  `tt pi clear` before the next dispatch.
- `idle` — anything else.

## Model tiers

Default tier is `low`; `--medium` on `send` is for safety-critical work.
Reasoning effort is a **runtime knob**: `send` writes the tier into the
trigger and the `tt-worker` extension applies it with
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
| `<cs>.tasks.jsonl` | One JSON line per turn: `{turn,id,sent_at,tier,nonce}`; plus a `{"clear":<gen>}` marker line per `clear`. |
| `<cs>.tier` | Current pi thinking tier. |
| `<cs>.gen` | Current context generation (bumped by `clear`). |
| `<cs>.in.<N>.txt` | Prompt body for turn N. |
| `<cs>.trigger` | Prompt handed to the REPL (id line + body). |
| `<cs>.trigger.consuming` | Transient: the trigger mid-consumption (renamed by the extension). |
| `<cs>.result` | Latest turn result (id / status / text). |
| `<cs>.starting` | Boot stamp (`date +%s`) written by `start_repl`; marks the REPL's async boot window for `repl_starting`. |
| `<cs>.ready` | Marker: the REPL's trigger watch is live. |
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
file/trigger control channel, so delivery uses tmux directly. `tt x send`
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

## Pool model v2 (proposed — not yet implemented)

> **Status: design only.** The live system is the immortal/cap/single-trigger
> model described above (v1). This section records the agreed successor so it
> survives design discussion; nothing here is built or tested yet.

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
  materialize on first dispatch. Baseline N=0; the always-there interactive
  REPL becomes opt-in via a spawn-only verb.

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
- **Drain priority:** a worker going idle (`agent_end`) claims its own queue
  first, then steals from the pool. Claim = atomic rename — the same primitive
  that already consumes triggers, so still **no daemon**.

### Three injection semantics

| Verb | Timing | Pinned |
|------|--------|--------|
| `steer <cs>` / `steer-all` | now, interrupts the current turn | yes |
| `send <cs>` (worker busy) | next, after the current turn | yes |
| `auto` (all busy at cap) | whenever any worker frees | no |

### Join + notify

- `tt pi wait-all [names…]` — block until all named (or all busy) workers reach a
  terminal result; one consolidated report. Replaces per-worker `wait` fan-out
  and is the main fix for v1's coordination context-cost. (`send-all` =
  broadcast-same-prompt convenience, secondary; distinct-task fan-out is just
  batched `send`s in one shell call.)
- `--notify` (on `send`/`auto`) — on completion, push `<id> done` into the
  `claude` pane via the `tt x send` paste path. Background dispatch + wake-up
  built from parts tt already owns; queued tasks survive a reboot, which an
  in-memory Workflow fan-out cannot.

### Cap

`min(cores-2, 26)` — 26 = NATO-letter exhaustion (`zulu`), and a **hard ceiling
for every path, manual and auto alike**. At cap, `auto` queues rather than
spawning and any explicit spawn is refused (it may warn as it approaches). The
ceiling is the runaway backstop that makes auto-spawn safe.

### Control-channel changes this forces

- The single-slot `<cs>.trigger` becomes a **queue dir**; bash always *appends*
  and the worker pulls when it knows it is truly idle — closing the TOCTOU race
  of bash deciding idle-vs-busy from outside. `trigger` becomes the
  "currently running" marker.
- `steer` is a separate immediate channel, bypassing the queue.
- When implemented, revise "The tt-worker extension", "Task IDs & completion",
  and "Worker state detection" above.

### Verbs removed / deferred

- `tt pi add` is **removed entirely** — spawning is implicit via `send`/`auto`
  and lazy spawn covers every case, so there is no spawn-only verb (a human
  wanting a bare REPL opens a window and runs pi, or sends a trivial task).
  `rm` (destroy) and `clear` (reset context) stay.
- Deferred until needed: `tt pi logs <cs>` (build when the orchestrator finds
  itself steering blind), a JSON result envelope (`wait --json`).

### Framing — the moat

It is **provider heterogeneity**, not flat-rate Codex: any orchestrator driving
a pool of any-provider workers (pi on Codex, Kimi, Deepseek, …), each on its own
account, quota, and model. Workflow's agents are Claude subagents on the
orchestrator's own budget and cannot follow there. The consumer
`delegating-to-pi` skill and `tt pi status` output should state this so the
orchestrator reaches for tt on heavy fan-out. (`tt x send` and the skill
generalize to non-Claude orchestrators — already on the roadmap.)

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

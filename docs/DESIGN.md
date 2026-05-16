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

(The original `pi -p` shell model is retired. It is preserved only in
git history.)

### `tt up` starts the REPLs asynchronously

`tt up` does not block on REPL readiness. It creates/heals the windows,
fires `start_repl` for every immortal (each just `respawn-pane`s the
pane and stamps `<cs>.starting`), launches the orchestrator, and
attaches the user immediately — the user lands in the `claude` window in
well under a second. The pi REPLs finish booting in the background.

The 40 s readiness wait is **lazy**: `tt pi send` calls
`ensure_repl_ready`, which waits for *that* worker's `<cs>.ready` only
when a task is actually dispatched. A `send` issued while the boot is
still in flight simply blocks until the worker is up — and because the
trigger is still written only after `<cs>.ready` is confirmed, the
startup trigger race stays closed. A `send` to a genuinely-dead worker
(no process, no recent `<cs>.starting`) re-starts it first.

## The tt-worker extension — control channel

tt talks to each REPL through **`tt-worker.ts`**, a pi extension
installed globally in `~/.pi/agent/settings.json`. It is inert unless
`TT_WORKER_CS` is set, so it has no effect on the user's ordinary pi
sessions; tt sets that env var (and `TT_WORKER_STATE`) only for the
workers it spawns.

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
   equals the task-id. `status` of `done`/`blocked` prints the assistant
   text and exits 0; `other` (pi answered without a marker) and `error`
   (extension exception) exit 1; `running` keeps polling. `BLOCKED` is
   classified ahead of `WORKER_DONE` so a real block is never masked by
   a trailing wrapper.
3. If the trigger is still sitting unconsumed 20 s after dispatch (the
   worker is wedged or its watch is dead), `wait` fails fast with a
   diagnostic instead of silently burning the full timeout.

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

- `.pi/APPEND_SYSTEM.md` — pi's Worker Mode rules, auto-appended from cwd.
- The `TASK / FILES / CHANGE / [CONTEXT] / SUCCESS` prompt format.
- The `WORKER_DONE` / `BLOCKED:` completion markers.
- The model ladder (`gpt-5.5:low` default, `:medium` for safety-critical).
- The `tt pi send` / `wait` interface — same verbs, same task-ids.

## Out of scope (deliberately)

- Auto-starting the dev server (`dev` window stays an empty shell).
- Per-project `tt` config (custom dev command / default tier).

## Files and external state

| Location | Purpose |
|----------|---------|
| `~/code/tt/tt` | The tool itself (symlinked from `~/.local/bin/tt`). |
| `~/code/tt/tt-worker.ts` | The pi extension; loaded globally via `~/.pi/agent/settings.json`. |
| `~/.local/share/tt/` | XDG data dir: symlinks to `.pi/`, `.agents/`, `tt-worker.ts`. |
| `~/.local/state/tt/<session>/` | XDG state dir: trigger/result/task files per worker. Override with `TT_STATE_DIR`. |
| `~/.pi/agent/settings.json` | Registers `tt-worker.ts` as a global extension; excludes `delegating-to-pi` skill. |
| `.pi/settings.json` | Project-local pi settings: repeats the skill exclusion for the project-discovered copy. |
| `.pi/APPEND_SYSTEM.md` | Project-local worker protocol injected into every pi REPL. If absent, the global `~/.local/share/tt/.pi/APPEND_SYSTEM.md` is used. |
| `.agents/skills/delegating-to-pi/` | Consumer-facing skill telling the orchestrator how to use `tt pi send`/`wait`. |

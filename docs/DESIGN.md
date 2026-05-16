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

(The original `pi -p` shell model is retired. It is preserved only in
git history.)

## The tt-worker extension — control channel

tt talks to each REPL through **`tt-worker.ts`**, a pi extension
installed globally in `~/.pi/agent/settings.json`. It is inert unless
`TT_WORKER_CS` is set, so it has no effect on the user's ordinary pi
sessions; tt sets that env var (and `TT_WORKER_STATE`) only for the
workers it spawns.

The extension and tt exchange two plain files under
`/tmp/tt/<session>/`, both in a trivial line format so the bash side
needs no JSON parser:

- **`<cs>.trigger`** — tt writes it: line 1 is `<task id> <tier>`, the
  rest is the prompt body. The extension's `fs.watchFile` fires, it
  applies the tier (`pi.setThinkingLevel`), the body is sent to the REPL
  as a user message (`pi.sendUserMessage`, steered if pi is mid-turn),
  and the file is truncated.
- **`<cs>.result`** — the extension writes it on every `agent_end`:
  ```
  id: <task id | ->
  status: done|blocked|other
  ---
  <last assistant text, verbatim>
  ```
  `id` is the trigger's id for a tt-injected turn, or `-` for a
  human-typed one — so a person typing into the REPL never confuses
  tt's `wait`.
- **`<cs>.ready`** — the extension touches it once its trigger watch is
  live, so `launch_repl` knows when it is safe to write a trigger.

## Task IDs & completion

1. `tt pi send` assigns the task id `<callsign>-<turn>` (turn = line
   count of `tasks.jsonl` + 1), writes the trigger, and appends
   `{turn,id,sent_at,tier}` to `tasks.jsonl`.
2. `tt pi wait <cs> <task-id>` polls `<cs>.result` until its `id` field
   equals the task-id, then prints the assistant text. `status` of
   `done`/`blocked` exits 0; `other` (pi answered without a marker) is
   an error. `BLOCKED` is classified ahead of `WORKER_DONE` so a real
   block is never masked by a trailing wrapper.

## Worker state detection

State is derived from the window plus the control files:

- `missing` — the window does not exist.
- `down` — window exists but no pi process is alive for it (matched by
  the worker's unique `--session-dir` path via `pgrep -f`; tmux's
  `pane_current_command` is unreliable because pi runs as a grandchild).
- `busy` — the last task id in `tasks.jsonl` has no matching id in
  `<cs>.result` yet.
- `blocked` — the last result's status is `blocked`.
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
fresh pi session — no `--continue`, no leftover context.

## State files

Under `/tmp/tt/<session>/`:

| File | Contents |
|------|----------|
| `<cs>.tasks.jsonl` | One JSON line per turn: `{turn,id,sent_at,tier}`. |
| `<cs>.tier` | Current pi thinking tier. |
| `<cs>.gen` | Current context generation (bumped by `clear`). |
| `<cs>.in.<N>.txt` | Prompt body for turn N. |
| `<cs>.trigger` | Prompt handed to the REPL (id line + body). |
| `<cs>.result` | Latest turn result (id / status / text). |
| `<cs>.ready` | Marker: the REPL's trigger watch is live. |
| `pi-sessions/<cs>/g<N>/` | pi `--session-dir` for generation N. |

## What does NOT change vs the old `pi -p` flow

- `.pi/APPEND_SYSTEM.md` — pi's Worker Mode rules, auto-appended from cwd.
- The `TASK / FILES / CHANGE / [CONTEXT] / SUCCESS` prompt format.
- The `WORKER_DONE` / `BLOCKED:` completion markers.
- The model ladder (`gpt-5.5:low` default, `:medium` for safety-critical).
- The `tt pi send` / `wait` interface — same verbs, same task-ids.

## Out of scope (deliberately)

- Auto-starting the dev server (`dev` window stays an empty shell).
- Auto-launching `claude` on `tt up`.
- Per-project `tt` config (custom dev command / default tier).

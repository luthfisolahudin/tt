# tt — design & rationale

## Why it exists

The orchestrator (Claude Code) delegates mechanical subtasks to **pi**, a
one-shot code worker wired to OpenAI Codex (ChatGPT Plus, flat-rate).
Originally the orchestrator ran `pi -p "..."` directly in Bash. Two problems:

1. **No visibility** — the user could not watch pi work in real time.
2. **No parallelism / no iteration** — every call was ephemeral and serial.

`tt` solves both by giving each project one tmux session that hosts the dev
server, the orchestrator, and a pool of pi workers. One place to attach; one
place to see everything.

## Session model

- **One tmux session per project**, named `<basename($PWD)>-<sha1($PWD)[:4]>`.
  Deterministic from the project path; the 4-char hash disambiguates
  same-named directories.
- **Standard windows**, created at session-up time (idempotent):

  | Idx | Name | Contents |
  |-----|------|----------|
  | 0 | `dev` | Empty shell in `$PWD`. Run the dev server here. |
  | 1 | `claude` | Empty shell. Launch the orchestrator here. |
  | 2 | `pi-alfa` | Pi worker shell. **Immortal.** |
  | 3 | `pi-bravo` | Pi worker shell. **Immortal.** |
  | 4 | `pi-charlie` | Pi worker shell. **Immortal.** |
  | 5+ | `pi-delta` / `pi-echo` | Optional, on-demand. Hard cap of 5 pi. |
  | tail | user windows | Ad-hoc, created with `Ctrl-b c`. Not managed by tt. |

- Attach lands on `claude`.
- `history-limit` is set to `50000` so `capture-pane` sees full transcripts.

## Pi windows are SHELLS, not a running pi REPL

The most important design decision. Each `pi-*` window hosts a plain bash
shell. `tt pi send` runs

```
pi -p --session-dir <dir> [--continue] --model openai-codex/gpt-5.5:<tier> < <promptfile>
```

inside that shell. Rationale:

- **No REPL input-quoting hell.** Feeding a multi-line TASK/FILES/CHANGE
  prompt into an interactive pi REPL via `tmux send-keys` is fragile
  (bracketed paste, multi-line submit ambiguity). A shell + file redirect
  is unambiguous.
- **Visibility is preserved** — pi's output streams to the pane; attach and
  watch.
- **Persistence is preserved** — `--session-dir` + `--continue` keep context
  across turns via pi's own session files. No need for a live process.

So a "worker" is really: a window + a pi session-dir + tt's bookkeeping.

## Worker flavors

- **Ephemeral** (default): `tt pi clear` → `send` → `wait`. `clear` wipes
  prior context (bumps a generation counter → new session-dir, no
  `--continue`).
- **Persistent**: skip `clear`. Reuse the worker for a short chain of
  bounded follow-ups; context accumulates via `--continue`.

## Task IDs & the watermark mechanism

`wait` must not false-positive on a stale `WORKER_DONE` from an earlier turn
in the same pane. Solution — a **watermark**, with no injection into pi's
input:

1. `tt pi send` captures the pane and records the **index of the last
   non-blank line** as the watermark, plus turn/tier/session-dir, into
   `/tmp/tt/<session>/<callsign>.tasks.jsonl`.
2. The task ID returned is `<callsign>-<turn>` (e.g. `bravo-3`).
3. `tt pi wait <cs> <task-id>` looks up that watermark and scans **only
   pane content past it** for `WORKER_DONE` / `BLOCKED:`.

pi never sees an extra marker or comment — the mechanism is entirely on the
tmux side.

> Two subtle bugs in this area were found and fixed during verification —
> see `docs/STATUS.md` ("Bugs found & fixed"). The watermark MUST count to
> the last non-blank line (not `wc -l`), and `send` MUST block until pi has
> actually launched. Do not regress these.

## Worker state detection

State is derived, no extra state file:

- `busy` — `tmux pane_current_command` for the window is `pi`.
- `blocked` — not busy, and pane tail past the last watermark has `BLOCKED:`.
- `idle` — anything else (incl. no task ever sent).
- `missing` — the window does not exist.

## Model tiers

Default tier is `low`. `--medium` on `send` is for safety-critical work.
Because pi windows are shells, switching tiers needs no respawn or probe —
`send` simply passes a different `--model openai-codex/gpt-5.5:<tier>` for
that turn. The tier is remembered in `<callsign>.tier` and sticks until the
next `--low`/`--medium` or a `clear`.

(An earlier plan described probing pi for a `/thinking` slash command and a
respawn fallback. The shell model made all of that unnecessary — it was
never implemented and is not needed.)

## State files

Under `/tmp/tt/<session>/`:

| File | Contents |
|------|----------|
| `<cs>.tasks.jsonl` | One JSON line per turn: `{turn,line,sent_at,tier,sdir}`. |
| `<cs>.tier` | Current pi thinking tier. |
| `<cs>.gen` | Current context generation (bumped by `clear`). |
| `<cs>.in.<N>.txt` | Prompt body for turn N. |
| `pi-sessions/<cs>/g<N>/` | pi `--session-dir` for generation N. |

## What does NOT change vs the old `pi -p` flow

- `.pi/APPEND_SYSTEM.md` — pi's Worker Mode rules, auto-appended from cwd.
- The `TASK / FILES / CHANGE / [CONTEXT] / SUCCESS` prompt format.
- The `WORKER_DONE` / `BLOCKED:` completion markers.
- The model ladder (`gpt-5.5:low` default, `:medium` for safety-critical).

## Out of scope (deliberately)

- Auto-starting the dev server (`dev` window stays an empty shell).
- Auto-launching `claude` on `tt up`.
- Streaming pi events over `--mode json` / JSON-RPC — tmux capture suffices.
- Per-project `tt` config (custom dev command / default tier).

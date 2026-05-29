# tt — tmux team

A single-file bash tool that gives every project **one tmux session** hosting
the dev server, the orchestrator (Claude Code), and a pool of **pi** code
workers — so there is one place to attach and watch everything.

`tt` is the substrate for delegating work to **pi** (a live REPL worker
wired to OpenAI Codex). Each worker runs a persistent, interactive pi REPL
in a visible tmux window — the orchestrator drives it via a per-worker task
queue and control files rather than one-shot `pi -p` calls.

- **Tool:** `~/code/tt/tt` (this repo) — symlinked from `~/.local/bin/tt`.
- **Design & rationale:** `docs/DESIGN.md`.

> **Contributors & AI agents:** read `CLAUDE.md` first, then `docs/STATUS.md`, before editing anything.

## Install

```sh
ln -s ~/code/tt/tt ~/.local/bin/tt
```

Dependencies: `tmux`, `sha1sum`/`sha256sum` (coreutils), `sed`, `awk`, `bash`.
`pi` must be on `PATH` for the worker verbs.

## Quick start

```sh
cd ~/code/my-project
tt                       # create + attach the project's tmux session
```

This creates a session named `<basename($PWD)>-<sha1($PWD)[:4]>` with two
windows: `dev` and `claude`. Run the dev server in `dev`; the orchestrator
(Claude Code) is auto-launched in `claude`. The worker pool is **lazy** — no
`pi-*` workers are pre-spawned; a worker's REPL is created on demand by the
first `tt pi send <cs>` or `tt pi auto`.

## Delegating to a pi worker

```sh
TID=$(tt pi send alfa - <<'PROMPT'
TASK: rename `foo` to `bar` in app/root.tsx
FILES: app/root.tsx
CHANGE: rename the local variable only; do not touch other identifiers.
SUCCESS: `pnpm typecheck` passes; no other diffs.
PROMPT
)
tt pi wait alfa "$TID"     # blocks, prints the WORKER_DONE / BLOCKED block
```

`send` prints a **task ID** (`alfa-3`); `wait` uses it to anchor on the
*current* turn's output and ignore stale markers from earlier turns.

## Command reference

Run `tt --help` for the full block. Summary:

| Verb | Effect |
|------|--------|
| `tt` / `tt up` | Create (if missing) + attach the project session. Idempotent. Attaches at once; pi REPLs boot in the background. |
| `tt a` / `tt attach` | Attach without creating. |
| `tt name` | Print the computed session name. |
| `tt --version`, `tt -v` | Print the installed `tt` version. |
| `tt down` | Kill the project session (with confirmation). |
| `tt pi clear [--force] <cs>` | Wipe a worker's pi-session context. Refuses unless idle/blocked. |
| `tt pi resume <cs>` | Recover an **interrupted** worker without a context wipe: re-drive its task to completion (`interrupted → busy → done`). Needs the REPL alive. In the worker's own pane, `/tt-resume` does the same. |
| `tt pi send [--low\|--medium] [--notify] <cs> (FILE\|-)` | Send a prompt; print task ID. Lazy-spawns an absent worker; queues behind a busy one (run-next). `--notify`: ping the orchestrator on completion. |
| `tt pi auto [--low\|--medium] [--rm] [--notify] (FILE\|-)` | Dispatch without naming a worker: reuse idle → spawn → shared pool. Echoes `using pi-<cs>`; prints the task ID. `--rm`: fresh ephemeral worker, reaped after. `--notify`: ping the orchestrator on completion. |
| `tt pi steer <cs\|all> (FILE\|-)` | Inject a message NOW into the current turn (run-now), bypassing the queue. Untracked. |
| `tt pi wait [--timeout N] [--json] <cs\|task-id\|pool-id\|all> [task-id]` | Block until `WORKER_DONE`/`BLOCKED:`. Accepts a callsign (latest task), a bare task-id (any id resolves, even an old one), a pool id, or `all` (join all busy). `--json`: result envelope(s). |
| `tt pi collect [--timeout N] [--json] [all\|<cs>]` | Cursor-based fan-out join: every result with turn past the per-worker cursor, blocking on in-flight ones, then advances the cursor. Never drops a task that finished before you asked (vs `wait all`, busy-now only). |
| `tt pi results [--json] [<cs>\|<task-id>]` | Read durable outcomes from the per-id store: list all (newest first), filter to a worker, or re-read one by id. Recovers an id you no longer have. |
| `tt pi status [--json]` | One row per worker: state, last task, tier, generation; interrupted/blocked rows carry a reason hint. |
| `tt pi rm [--force] <cs>`, `tt pi remove [--force] <cs>` | Remove a worker (kill REPL + window, wipe state incl. its durable results). |
| `tt pi popidle` | Remove the highest-NATO idle worker. |
| `tt x send [--timeout N] <session-id> (FILE\|-)` | Wait for another session's orchestrator to safely accept input, then send + submit a message. Waits forever by default. |
| `tt x ls [--all]`, `tt x list [--all]` | List tt sessions available to message. Default: only sessions with a live orchestrator. `--all`: show all with status. |
| `tt x observe [run] [--interval N] [--duration N] [--all]` | Passively sample Claude panes to SQLite for improving `tt x send` safe-input detection; duplicate non-`ts` payloads are ignored. |

Workers are lazy: callsigns `alfa` through `zulu` (NATO) are spawned on demand
by `send`/`auto` and torn down by `rm`/`popidle` (or `--rm`). None is special or
pre-spawned. Hard cap `min(cores-2, 26)`.

## Checking a session's tt version

`tt --version` shows the executable currently on your `PATH`. `tt up` also
stamps the live tmux session with the version that last managed it:

```sh
tmux show-environment -t "=$(tt name)" TT_VERSION
cat "${XDG_STATE_HOME:-$HOME/.local/state}/tt/$(tt name)/version"
```

Newly spawned pi worker REPLs also receive `TT_VERSION` in their process
environment. See `CHANGELOG.md` for what changed between versions.

## Consumers

Any project can adopt `tt`. Wire it up by adding a `delegating-to-pi`
skill (`SKILL.md`, `AGENTS.md` / `CLAUDE.md`) that tells the orchestrator
to delegate via `tt pi send` / `tt pi wait`.

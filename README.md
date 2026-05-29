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

This creates a session named `<basename($PWD)>-<sha1($PWD)[:4]>` with five
windows: `dev`, `claude`, `pi-alfa`, `pi-bravo`, `pi-charlie`. Run the dev
server in `dev`; the orchestrator (Claude Code) is auto-launched in `claude`;
the three `pi-*` windows are immortal pre-spawned workers.

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
| `tt pi add` | Spawn the next worker (`delta`, `echo`, …, `zulu`). Cap `min(cores-2, 26)`. |
| `tt pi clear [--force] <cs>` | Wipe a worker's pi-session context. Refuses unless idle/blocked. |
| `tt pi send [--low\|--medium] <cs> (FILE\|-)` | Send a prompt; print task ID. Lazy-spawns an absent worker; queues behind a busy one (run-next). |
| `tt pi steer <cs\|all> (FILE\|-)` | Inject a message NOW into the current turn (run-now), bypassing the queue. Untracked. |
| `tt pi wait [--timeout N] <cs\|all> [task-id]` | Block until `WORKER_DONE`/`BLOCKED:`. Task-id optional (defaults to latest). `wait all` joins all busy workers. |
| `tt pi status` | One row per worker: state, last task, tier, generation. |
| `tt pi rm [--force] <cs>`, `tt pi remove [--force] <cs>` | Remove a non-immortal worker. |
| `tt pi popidle` | Remove the highest-NATO idle non-immortal worker. |
| `tt x send [--timeout N] <session-id> (FILE\|-)` | Wait for another session's orchestrator to safely accept input, then send + submit a message. Waits forever by default. |
| `tt x ls [--all]`, `tt x list [--all]` | List tt sessions available to message. Default: only sessions with a live orchestrator. `--all`: show all with status. |
| `tt x observe [run] [--interval N] [--duration N] [--all]` | Passively sample Claude panes to SQLite for improving `tt x send` safe-input detection; duplicate non-`ts` payloads are ignored. |

Workers: `alfa`, `bravo`, `charlie` are immortal (always present); `delta`
through `zulu` are optional, spawned on demand. Hard cap `min(cores-2, 26)`.

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

# tt — tmux team

A single-file bash tool that gives every project **one tmux session** hosting
the dev server, the orchestrator (Claude Code), and a pool of **pi** code
workers — so there is one place to attach and watch everything.

`tt` is the substrate for delegating work to **pi** (a live REPL worker
wired to OpenAI Codex). Each worker runs a persistent, interactive pi REPL
in a visible tmux window — the orchestrator steers it via trigger files
rather than one-shot `pi -p` calls.

- **Tool:** `~/code/tt/tt` (this repo) — symlinked from `~/.local/bin/tt`.
- **Design & rationale:** `docs/DESIGN.md`.

> **Contributors & AI agents:** read `CLAUDE.md` first, then `docs/STATUS.md`, before editing anything.

## Install

```sh
ln -s ~/code/tt/tt ~/.local/bin/tt   # already done on this machine
```

Dependencies: `tmux`, `sha1sum` (coreutils), `sed`, `awk`, `bash`. `pi` must
be on `PATH` for the worker verbs.

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
TID=$(tt pi send alfa <(cat <<'PROMPT'
TASK: rename `foo` to `bar` in app/root.tsx
FILES: app/root.tsx
CHANGE: rename the local variable only; do not touch other identifiers.
SUCCESS: `pnpm typecheck` passes; no other diffs.
PROMPT
))
tt pi wait alfa "$TID"     # blocks, prints the WORKER_DONE / BLOCKED block
```

`send` prints a **task ID** (`alfa-3`); `wait` uses it to anchor on the
*current* turn's output and ignore stale markers from earlier turns.

## Command reference

Run `tt --help` for the full block. Summary:

| Verb | Effect |
|------|--------|
| `tt` / `tt up` | Create (if missing) + attach the project session. Idempotent. |
| `tt a` / `tt attach` | Attach without creating. |
| `tt name` | Print the computed session name. |
| `tt down` | Kill the project session (with confirmation). |
| `tt pi add` | Spawn the next worker (`delta`, then `echo`). Cap of 5. |
| `tt pi clear <cs> [--force]` | Wipe a worker's pi-session context. Refuses unless idle/blocked. |
| `tt pi send <cs> [--low\|--medium] (FILE\|-)` | Send a prompt; print task ID. |
| `tt pi wait <cs> <task-id> [--timeout N]` | Block until `WORKER_DONE`/`BLOCKED:`. |
| `tt pi status` | One row per worker: state, last task, tier, generation. |
| `tt pi down <cs> [--force]` | Remove a non-immortal worker. |
| `tt pi popidle` | Remove the highest-NATO idle non-immortal worker. |

Workers: `alfa`, `bravo`, `charlie` are immortal (always present); `delta`,
`echo` are optional. Hard cap of 5.

## Consumers

`tt` is referenced by the **bassaudio-storefront** project (its first
adopter):

- `.agents/skills/delegating-to-pi/SKILL.md` — tells the orchestrator to
  delegate via `tt pi send` / `tt pi wait`.
- `AGENTS.md` / `CLAUDE.md` — a "Tmux session" subsection pointing at `tt`.

Any project can adopt `tt`; it is not project-specific.

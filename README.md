# tt — tmux team

A single-file bash tool that gives every project **one tmux session** hosting
the dev server, the orchestrator (Claude Code), and a pool of **pi** code
workers — so there is one place to attach and watch everything.

`tt` is the substrate for delegating work to **pi** code workers. Each worker
runs a persistent, interactive pi REPL in a visible tmux window — the
orchestrator drives it via a per-worker task queue and control files rather
than one-shot `pi -p` calls. The model comes from the dispatched **tier**
(see [Model tier](#model-tier)).

- **Tool:** `~/code/tt/tt` (this repo) — symlinked from `~/.local/bin/tt`.
- **Design & rationale:** `docs/DESIGN.md`.

> **Contributors & AI agents:** read `CLAUDE.md` first, then `docs/STATUS.md`, before editing anything.

## Install

```sh
ln -s ~/code/tt/tt ~/.local/bin/tt
```

Dependencies: `tmux`, `bash`, coreutils (`sha1sum`/`sha256sum`, `stat`, `date`),
`sed`, `awk`, `perl`. `pi` must be on `PATH` for the worker verbs. Also:
`jq` for `.tt/windows.json` (without it tt falls back to the built-in layout),
`sqlite3` for `tt x observe`.

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

### Custom window layout (`.tt/windows.json`)

Drop a `.tt/windows.json` in a project to make `tt up` build its fixed windows
for you — split panes, pre-run commands, swap the orchestrator agent. Absent
file → the built-in `dev` + `claude` default. Schema: [`docs/windows.schema.json`](docs/windows.schema.json).

```jsonc
{
  "dev": {
    "layout": "even-horizontal",
    "panes": [
      { "cmd": "pnpm dev" },                                   // split + type + Enter
      { "cmd": "pnpm emu" },
      { "cmd": "./scripts/tunnel.sh", "enter": false }         // pre-type only; you press Enter
    ]
  },
  "claude": { "panes": [ { "cmd": "pi" } ] }                   // different orchestrator agent
}
```

- A pane `cmd` is sent **only into a bare shell** (idempotency guard), so re-running
  `tt up` never doubles panes or re-injects into a running process — and a crashed
  daemon (`enter:true`) is relaunched on the next `tt up`. `enter:false` panes are
  pre-typed once on creation and never re-sent.
- `dev`/`claude` are *roles*; `claude` is always the attach/focus target. Override
  `claude.panes[0].cmd` to launch any agent. Add more windows via `extra_windows`.
- The lazy `pi-*` worker pool is **not** configured here — that stays tt-owned.
- Requires `jq`; without it tt ignores the file and uses the legacy `dev`+`claude` layout.

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

### Hiding instructions from pi workers

Workers load the same discovered `AGENTS.md` / `CLAUDE.md` context as pi, but
the `tt-worker` extension strips sections wrapped in these markers before a
worker turn reaches the model:

```md
<!-- pi-worker:exclude-start -->
Orchestrator-only guidance goes here.
<!-- pi-worker:exclude-end -->
```

Use this in ancestor/global context files for instructions that should guide the
orchestrator but not delegated pi workers. Respawn existing workers with
`tt pi clear <cs>` after upgrading the extension.

## Command reference

Run `tt --help` for the full block. Summary:

| Verb | Effect |
|------|--------|
| `tt` / `tt up` | Create (if missing) + attach the project session. Idempotent. No pi workers are pre-spawned — the pool is lazy. |
| `tt a` / `tt attach` | Attach without creating. |
| `tt name` | Print the computed session name. |
| `tt --version`, `tt -v` | Print the installed `tt` version. |
| `tt --help`, `tt -h` | Full reference block. |
| `tt down` | Kill the project session (with confirmation). |
| `tt pi clear [--force] <cs>` | Wipe a worker's pi-session context. Refuses while the worker is `busy`. |
| `tt pi resume <cs>` | Recover an **interrupted** worker without a context wipe: re-drive its task to completion (`interrupted → busy → done`). Needs the REPL alive. In the worker's own pane, `/tt-resume` does the same. |
| `tt pi send [--tier NAME] [--notify] <cs> (FILE\|-)` | Send a prompt; print task ID. Lazy-spawns an absent worker; queues behind a busy one (run-next). `--tier NAME`: pick a model preset (see [Model tier](#model-tier)) — **refused** if the worker already runs a different tier. `--notify`: ping the orchestrator on completion. |
| `tt pi auto [--tier NAME] [--prefer-fresh] [--rm] [--notify] [--json] (FILE\|-)` | Dispatch without naming a worker: reuse idle → spawn → shared pool. Echoes `using pi-<cs>`; prints the task ID. The only accepted tier is `default`, so normally omit `--tier`. `--prefer-fresh`: spawn a new worker before reusing an idle one (parallelism + clean context), under the cap. `--rm`: fresh ephemeral worker, reaped after. `--notify`: ping the orchestrator on completion. `--json`: emit `{worker,task_id,routed}` (routed = `idle\|spawn\|pool\|ephemeral`). |
| `tt pi steer <cs\|all> (FILE\|-)` | Inject a message NOW into the current turn (run-now), bypassing the queue. Untracked. |
| `tt pi steer-all (FILE\|-)` | Steer every live worker. Same as `tt pi steer all`. |
| `tt pi wait [--timeout N] [--json] <cs\|task-id\|pool-id\|all> [task-id]` | Block until `WORKER_DONE`/`BLOCKED:`. Accepts a callsign (latest task), a bare task-id (any id resolves, even an old one), a pool id, or `all` (join all busy). `--json`: result envelope(s). `all` prints a one-line tally on stderr and exits non-zero if any worker ended error/other/down/timeout. |
| `tt pi wait-all [--timeout N] [--json] [<cs>...]` | Join a specific set of workers (`tt pi wait all` can only join *every* busy worker). No callsigns → all currently-busy workers. |
| `tt pi collect [--timeout N] [--json] [all\|<cs>]` | Cursor-based fan-out join: every result with turn past the per-worker cursor, blocking on in-flight ones, then advances the cursor. Never drops a task that finished before you asked (vs `wait all`, busy-now only). |
| `tt pi results [--json] [<cs>\|<task-id>]` | Read durable outcomes from the per-id store: list all (newest first), filter to a worker, or re-read one by id. Recovers an id you no longer have. |
| `tt pi logs [--lines N] <cs>` | Dump a worker's pi REPL pane scrollback (read-only; default 200 lines) — tell an in-flight turn from a wedged one without attaching. |
| `tt pi status [--json]` | One row per worker: state, **elapsed** (in-flight turn time when busy), **queue depth** (`+N` pinned tasks waiting), last task, tier, generation; interrupted/blocked rows carry a reason hint. `--json` adds `elapsed_s`/`queued`. |
| `tt pi rm [--force] <cs>`, `tt pi remove [--force] <cs>` | Remove a worker (kill REPL + window, wipe state incl. its durable results). |
| `tt pi popidle` | Remove the highest-NATO worker that is not `busy`. |
| `tt pi update [<args>...]` | Run `pi update` against the worker's private `PI_CODING_AGENT_DIR` (the worker pool's installed extensions get updated, not the orchestrator's pi config). Forwards all args and exit code. No `tt` session required. |
| `tt x send [--timeout N] <session-id> (FILE\|-)` | Wait for another session's orchestrator to safely accept input, then send + submit a message. Waits forever by default. |
| `tt x ls [--all]`, `tt x list [--all]` | List tt sessions available to message. Default: only sessions with a live orchestrator. `--all`: show all with status. |
| `tt x observe [run] [--interval N] [--duration N] [--all]` | Passively sample Claude panes to SQLite for improving `tt x send` safe-input detection; duplicate non-`ts` payloads are ignored. |

Workers are lazy: callsigns `alfa` through `zulu` (NATO) are spawned on demand
by `send`/`auto` and torn down by `rm`/`popidle` (or `--rm`). None is special or
pre-spawned. Hard cap `min(cores-2, 26)`, floored at 1.

Worker states: `idle` · `busy` · `blocked` · `interrupted` · `starting` · `down` · `missing`.

### Model tier

A **tier** is a named preset bundling *(model, thinking effort)*. Thinking effort
is **fixed per tier** and cannot be set independently. Pass `--tier NAME` on
`tt pi send` / `tt pi auto` to pick one; omit `--tier` to keep the worker's
current tier (a fresh worker starts on the default):

| Tier | Model | Thinking effort | Role |
|------|-------|-----------------|------|
| `default` | `cosmoshub/qwen-3.7-max` | `max` | All delegated worker tasks. |

The worker model is text-only. Pi's shared CosmosHub model registry exposes
image-capable Gemini models, but `tt` does not route workers to them. The dated
benchmark, price snapshot, and re-evaluation rules live in
[`docs/MODEL_DECISION.md`](docs/MODEL_DECISION.md).

The legacy `--low`/`--medium`/`--high`/`--xhigh`/`--max` flags are rejected with
a pointer to `--tier`. See the prompting guide in
`skills/delegating-to-pi/references/`.

**A tier cannot be changed on a running worker.** The model is baked into the
REPL's `--model` at launch, so `tt pi send --tier NAME <cs>` against a worker
already on a different tier (if the registry later gains one) is *refused*
rather than silently dispatched to the
wrong model. Respawn on the new tier with `tt pi clear <cs>` (loses context).
`tt pi auto --tier NAME` sidesteps this by skipping non-matching idle workers
and spawning fresh; at the cap, with no compatible worker free, it refuses too.
After a tier is removed, existing workers on it show `stale:<name>` and reject
new work until `tt pi clear <cs>` respawns them on the current default.

Custom-provider credentials needed inside worker REPLs are synchronized from
the current shell into the tmux session on `tt up` and before worker spawn.
`TT_PI_ENV_VARS` is a space-separated allowlist; values are not written to tt
state files.

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

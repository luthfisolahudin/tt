# AGENTS.md — tt

`tt` ("tmux team") is a Go tool: per-project tmux session + a pool of `pi`
code workers, driven through a single background daemon (`ttd`). This file
orients an AI agent working **on** `tt` itself.

## Read first

1. `docs/STATUS.md` — current state, what is tested, what is not.
   **Always read before editing.**
2. `docs/DESIGN.md` — design and rationale (the live-REPL model and the
   `tt-worker` extension control channel).
3. `docs/PLAN.md` — the daemon/pipeline upgrade and its phases.
4. `README.md` — user-facing usage and command reference.

## Layout

- `main.go` · `cmd/` — the cobra CLI (one file per verb group). Verbs
  hand-parse flags (`DisableFlagParsing`) so flags may sit in any position,
  matching the historical bash surface; each `Run` therefore starts with a
  `helpRequested(args)` → `showHelp(cmd)` intercept.
- `internal/daemon/` — `ttd`: one process serving **all** sessions over a unix
  socket. Owns the worker-dispatch write side, the result watch side, the
  pipeline engine, and cross-session `x`.
- `internal/worker/` — dispatch/queue/result/state primitives and the tier
  registry. `internal/session/` — session naming, state dirs, `up`/`down`.
  `internal/tmux/` — tmux command wrappers. `internal/client/` — socket client
  + daemon lifecycle.
- `pi-worker/` — worker-only pi runtime templates. `extensions/tt-worker.ts`
  is the pi extension `tt` drives the REPLs through, auto-loaded by pi from
  tt's private `PI_CODING_AGENT_DIR`.
- `~/.local/bin/tt` is the **installed binary** — there IS a build step now:
  `make cutover` (or `make install` for a side-by-side `tt-go`).
- `docs/` — design, status, plan. `CHANGELOG.md` — version history (newest
  first).

## Conventions & invariants — do not regress

- **`set-option` targets use the bare-name `"$s:"` form**, not `"=$s"`.
  The `=` exact-match prefix is rejected by `set-option`.
- **Never `tmux attach` when `$TMUX` is set** — use `switch-client` (see
  `enterSession`).
- **`tt up` inside tmux does NOT switch by default.** An unsolicited
  `switch-client` replaces the caller's window (it stole a live window twice).
  In-tmux `up` builds/heals and stays put; `--attach` switches; outside tmux
  `up` attaches. This is a deliberate divergence from the retired bash tool.
- **Fixed windows come from `<project>/.tt/windows.json`** (normalized by the
  `normalizeJQ` program via `jq`; absent → built-in `dev`+`claude` default, same
  code path). Pane commands MUST stay **bare-shell-guarded** (`paneIsBare`) so
  re-`up` is idempotent and reboots self-heal; `enter:false` prefills fire only
  on fresh panes (cold start / created / just split), never on heal. Target
  panes by `pane_id`, never index (`pane-base-index` may be 1). Heal at
  **window** granularity — do not re-split a partially-closed window. The
  `pi-*` pool is NOT configurable here.
- **Pi windows host a live interactive pi REPL**, not a shell. `tt`
  drives them via the `tt-worker` extension's trigger/result files —
  never by `capture-pane` scraping (the retired watermark model was the
  source of every hard bug). `tt peek` / `tt pi logs` capture panes
  **read-only** and never drive a worker. See DESIGN.
- **REPL liveness is detected with `pgrep -f` on the worker's
  `--session-dir`**, never `pane_current_command` — pi is a grandchild
  process and tmux reports the foreground command inconsistently.
- **The daemon must stay a single writer for turn assignment.** Ops that
  mutate state (`send`/`auto`/`clear`/`rm`/`popidle`/`status`) hold `writeMu`;
  long-running ops (`wait`/`collect`/`pipeline`) must NOT hold it for their
  lifetime — the pipeline locks only its per-dispatch critical section.
- **On-disk state is the source of truth.** The daemon holds no authoritative
  in-memory state, so it is restartable and idempotent; a dead daemon degrades
  to "CLI can't reach it", never to lost work.
- **The control-channel file formats are a contract with the extension** —
  queue task head (`<id> <tier> <nonce>[ notify]`), `tasks.jsonl`, the
  `results/<id>.result` head, and the steer/resume triggers. Write them
  atomically (`.tmp` + rename). Changing them means changing
  `pi-worker/extensions/tt-worker.ts` too.
- **The extension spawns `tt pi notify-drain <session>`** on `--notify`
  completion — that verb must keep existing and keep its argument shape.
- **`pi-worker/extensions/tt-worker.ts` must stay inert unless `TT_WORKER_CS` is set** — this
  is a safety belt even though workers now use a private pi worker dir.
- **The `delegating-to-pi` skill must stay excluded from pi workers** —
  `pi-worker/settings.json` excludes it, and `tt` launches worker REPLs
  with `--no-skills` so project/user-discovered skills cannot make a
  delegate become the orchestrator.
- The pool is **lazy**: `tt up` pre-spawns no workers; a REPL is created on the
  first `tt pi send`/`auto`. No immortal caste — every NATO callsign
  (`alfa`…`zulu`) is ordinary and removable. Hard worker cap `min(cores-2, 26)`
  (`PiCap`), enforced on every spawn path.
- **Command synopsis style** (cobra `Use` + README table): flags before
  positionals, source last — `tt <verb> [FLAGS] <positionals> (FILE|-)`; and
  list the short/primary alias first (`ls`/`list`, `rm`/`remove`). Parsers
  accept flags in any position, so synopsis order is documentation only.
- **Every verb answers `--help` with exit 0.** A subcommand `--help` that
  errors is a bug — help is the discovery surface for the AI agents that drive
  this tool.

## Testing

`go build ./...`, `go vet ./...`, and `go test ./...` must all be clean —
run them after every change. Beyond that, verification is manual against a
throwaway `/tmp/tt-test-*` project; the procedure is in `docs/STATUS.md`.
Live `pi` steps spend real model quota — keep test tasks trivial.

Two legs cannot be verified without a live **Claude Code** TUI: `x send`
delivery and `notify-drain` delivery. The safe-input classifier only accepts
Claude Code's TUI markers — it does not recognize other TUIs (an `opencode`
orchestrator times out), and a `cat` stand-in never classifies as safe.

## Consumers

Consumer projects reference `tt` via a `delegating-to-pi` skill
(`SKILL.md`, `AGENTS.md`, `CLAUDE.md`) that tells the orchestrator to
delegate via `tt pi send` / `tt pi wait`. If you change the `tt pi`
interface, update that skill too.

## AI quick reference

```sh
tt pi send alfa - <<'P'
TASK: ...
FILES: path/to/file
CHANGE: ...
SUCCESS: ...
P
                               # dispatch; prints task-id like "alfa-3"
                               # (queues behind a busy worker; lazy-spawns absent)
tt pi wait alfa                # block on alfa's latest task (task-id optional)
TID=$(tt pi auto - <<<'...')   # let tt pick a worker (idle→spawn→pool); echoes "using pi-X"
TID=$(tt pi auto --rm - <<<'...')  # fresh ephemeral worker, reaped after
tt pi send --notify alfa - <<<'...'  # fire-and-forget; pings orchestrator on done
tt pi wait "$TID"              # waits on alfa-3 or pool-3 alike
tt pi steer alfa - <<<'...'    # inject NOW into the current turn (run-now)
tt pi wait all                 # fan-out join across all busy workers
tt pi collect --digest         # join a fan-out as one line per result
tt peek dev                    # read any window's pane content (read-only)
tt pipeline run spec.json      # declarative fan-out + review-gate pipeline
tt pi status                   # show all workers: state, last task, tier, gen
tt pi clear alfa               # wipe context; required before reuse
make check                     # build + vet + test after editing
```

Inline prompts use `-` (stdin) with a heredoc/here-string — `tt pi send alfa -
<<<'TASK: ...'` — not process substitution.

Worker states: `idle` · `busy` · `blocked` · `interrupted` · `starting` · `down` · `missing`

## Commit etiquette

Commit only when the user asks.

## Versioning

`tt` carries `MAJOR.MINOR.PATCH` in `internal/version/version.go` (pre-1.0, so
MAJOR stays `0`). Bump once per coherent change set — a feature plus its
follow-up fixes/docs share one version, not one bump per commit.

- **PATCH** (`0.3.x`) — the default: a new `tt pi`/`tt x` verb or flag, a
  behavior change, or a bug fix.
- **MINOR** (`0.x.0`) — a cross-cutting shift in the worker model, state layout,
  or runtime (e.g. the live-REPL rewrite, the XDG state move, the Go/daemon
  rewrite).
- **MAJOR** — reserved for post-1.0.

To bump: edit `internal/version/version.go`, add a `CHANGELOG.md` entry (newest
first), commit, then tag the commit — `git tag -a v<x.y.z> -m "tt v<x.y.z> —
<summary>"`. Tags let you diff releases (`git diff v0.15.0 v0.16.0`).
`docs/STATUS.md` tracks only current state, never history.

## When changing X, update Y

| Change | Also update |
|--------|-------------|
| `tt pi` verbs or flags | `README.md` command table · cobra `Use`/`Short` · consumer skill `SKILL.md` |
| trigger/result file format or nonce protocol | `docs/DESIGN.md` control channel + task IDs sections · `pi-worker/extensions/tt-worker.ts` |
| worker states | `docs/DESIGN.md` worker state detection section |
| daemon ops or socket protocol | `docs/DESIGN.md` · `docs/PLAN.md` |
| install layout (`~/.local/share/tt/`, `Makefile`) | `docs/STATUS.md` current state |
| completion markers (`WORKER_DONE` / `BLOCKED`) | `docs/DESIGN.md` · consumer skill `SKILL.md` · `pi-worker/APPEND_SYSTEM.md` |
| model tiers or provider | `docs/DESIGN.md` model tier · `docs/MODEL_DECISION.md` · `README.md` model tier table · consumer skill `references/prompting-and-tiers.md` + `references/prompting-<tier>.md` |
| version bump | `CHANGELOG.md` entry · `git tag -a v<x.y.z>` (see Versioning) |

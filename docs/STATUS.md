# tt — status & handoff

Read before touching `tt`. Design rationale lives in `docs/DESIGN.md`; version
history in `CHANGELOG.md`.

## Current state

- Single bash file (`~/code/tt/tt`, symlinked from `~/.local/bin/tt`) plus
  worker templates under `pi-worker/` and the consumer delegation skill under
  `skills/delegating-to-pi/`. State lives under
  `${XDG_STATE_HOME:-$HOME/.local/state}/tt/<session>/` (override `TT_STATE_DIR`);
  worker runtime under `${XDG_DATA_HOME:-$HOME/.local/share}/tt/pi-worker`
  (override `TT_PI_WORKER_DIR`). The global skill entry
  `~/.agents/skills/delegating-to-pi` is a symlink to the repo skill. See DESIGN
  "Files and external state".
- `tt up` builds the fixed windows (default `dev`/`claude`, or whatever
  `<project>/.tt/windows.json` declares — see README "Custom window layout" and
  `docs/windows.schema.json`), launches the orchestrator, attaches. Pane commands
  are bare-shell-guarded so re-`up` is idempotent and reboots self-heal; healing
  is at window granularity; panes are targeted by `pane_id` (safe under
  `pane-base-index 1`). Needs `jq` for the config path; without it the legacy
  `dev`/`claude` layout is used. The worker pool is lazy — no REPLs are
  pre-spawned; the first `tt pi send`/`auto` spawns the worker and waits for its
  readiness. `up` also stamps `TT_VERSION` into the session env and
  `$(state_dir)/version`.
- `tt pi wait` and `tt x send` wait forever by default; `--timeout N` bounds
  them. Internal health guards stay finite — notably a 20 s fast-fail on an
  unconsumed trigger.
- The single `default` tier routes all workers to CosmosHub Qwen 3.7 Max at max
  effort through Pi's Anthropic Messages-compatible custom provider. Normal
  dispatches omit `--tier`; the registry remains data-driven so a future model
  decision changes one row. The legacy
  `--low`/`--medium`/`--high`/`--xhigh`/`--max` flags are **rejected**
  (thinking effort is fixed per tier, not independently settable). See
  the "Model tier" section and `docs/MODEL_DECISION.md`.
- `tt x send` / `tt x list` / `tt x observe` provide cross-session messaging plus
  classifier-tuning diagnostics. See DESIGN.
- **Results are durable and id-addressable.** Every task — named and pool
  alike — records to `results/<id>.result`; `<cs>.result` is just the worker's
  latest-pointer for liveness. `tt pi wait <id>` resolves any id (older ones too);
  `tt pi results` re-reads outcomes after the fact; `tt pi collect` joins a
  fan-out via a per-worker cursor without dropping already-finished tasks;
  `--json` on `wait`/`status`/`results`/`collect` emits a stable envelope.
- **Interrupted workers recover in place.** `tt pi resume <cs>` (or the
  in-pane `/tt-resume`) re-drives an interrupted task to completion without a
  context wipe (`interrupted → busy → done`), via a `<cs>.resume` trigger the
  extension consumes; `tasks.jsonl` carries `notify` so resume re-honors
  `--notify`. `clear` still wipes; reset-to-idle was scoped out.
- **In-flight is observable (0.10.2–0.10.4).** `tt pi status` shows per-worker
  ELAPSED (busy-turn time from the `<cs>.busy` mtime) and QUEUE depth (`--json`
  adds `elapsed_s`/`queued`). The extension stamps `started_at`/`ended_at` into
  `results/<id>.result`, surfaced as `duration_s` in `--json` and a `DUR` column
  in `tt pi results` (older records read `null`/`-`). `tt pi auto` gains
  `--prefer-fresh` (spawn-before-reuse under cap: parallelism + clean context)
  and `--json` (`{worker,task_id,routed}`). `tt pi wait all` hints at `collect`
  when a non-busy worker holds a skipped result. `tt pi logs [--lines N] <cs>`
  dumps a worker's REPL scrollback read-only.
- **Worker-only context exclusions (0.10.6).** The `tt-worker` extension strips
  `<!-- pi-worker:exclude-start -->` … `<!-- pi-worker:exclude-end -->` blocks
  from loaded `AGENTS.md`/`CLAUDE.md` context before each tt-spawned worker turn,
  so ancestor/global docs can hold orchestrator-only guidance. Existing workers
  must be respawned (`tt pi clear <cs>`) to load the updated extension.
- **Default auto tier regression fixed (0.13.2).** `tt pi auto` and `auto --rm`
  again use `PI_TIER_DEFAULT` when `--tier` is omitted instead of aborting on the
  stale, unset `PI_DEFAULT_TIER` name.
- **CosmosHub default tier (0.14.1).** The former provider/model-named tiers are
  replaced by one model-agnostic `default` tier. The registry is now one
  data-driven list in `tt`; the worker extension no longer mirrors tier names or
  changes thinking effort at task claim. The selection evidence and future
  decision procedure live in `docs/MODEL_DECISION.md`. `tt up` and
  worker spawn synchronize the `TT_PI_ENV_VARS` allowlist (default:
  `COSMOSHUB_API_KEY`) into the tmux session so custom-provider auth reaches
  worker REPLs without being stored in tt state. The current private
  `pi-multi-auth` config hides `cosmoshub`, which makes the extension pass this
  environment-authenticated provider through instead of attempting rotation.
  Existing workers from an older tier registry are labeled `stale:<name>` and
  blocked from new work until `tt pi clear <cs>` respawns them on `default`.

## Verified (manual)

Exercised live against throwaway `/tmp/tt-test-*` projects and the repo's own
session — what a handoff can trust without retesting:

- Cold `tt up` builds `dev`/`claude` only (no pi-* pre-spawned); re-running
  heals missing/dead standard windows and never duplicates.
- send → wait happy path (`<cs>.result` transits `running`→`done`); BLOCKED path;
  stale-WORKER_DONE rejection (terminal-position + nonce); interrupted
  quarantine; runtime tier switch (low/medium live-tested); multi-turn context
  retention.
- lazy-spawn on first `send`/`auto`; `rm`/`popidle`; the `min(cores-2,26)` cap.
- `tt down` tears down session + state with no orphaned pi grandchildren.
- 20 s unconsumed-trigger fast-fail; `status: error` channel (the extension-side
  error writes themselves remain code-reviewed only).
- `tt x send` delivery (multiline, shell metachars, 4 KB bodies, FILE/stdin)
  against `cat` and a live orchestrator; `tt x list` / `ls --all`.
- **Records/recovery, verified live 2026-05-29** against the
  repo's own session: a real pi turn populating `results/<id>.result`;
  `wait --json` envelope; `tt pi results` listing; `notify` in `tasks.jsonl`;
  `tt pi collect` returning both tasks then advancing the cursor (re-collect =
  "nothing new"); and the headline `tt pi resume` recovery — a turn interrupted
  via Esc in the pane (→ `interrupted`/`status: other`) re-driven to `done` on
  the same REPL with context intact (`interrupted → busy → done`, same task id,
  nonce re-validated). `rm` wipes the worker's `results/<cs>-*` too. (The
  `--json`/parse/escape paths and cursor edges were additionally exercised
  against fabricated result files; the `status` reason hint is covered by those
  - logic — the live interrupt landed before any assistant text, so its body was
  empty and no hint was shown, which is correct.)
- **Completion-footer robustness (0.10.1), verified live 2026-05-29** against the
  repo's own session: a turn whose `WORKER_DONE` footer carried a **multi-line
  `notes:` value** (the exact shape that previously scored `other`/`interrupted`)
  now classifies `done` — the validator trusts the per-task nonce and tolerates
  multi-line values/trailing prose. `tt pi wait all`'s one-line tally lands on
  **stderr** (`wait-all: N task(s) — …`) while stdout stays the joined bodies, and
  exit code follows the documented contract. The negative guard (no-footer
  interrupt / wrong nonce → still `other`) is unchanged by construction — `status`
  defaults to `other` and only flips on a matching nonce at the terminal marker —
  but was not re-exercised live this round (needs a manual Esc).
- **Observability layer (0.10.2–0.10.4), verified live 2026-05-29** against the
  repo's own session: `tt pi status` showed `busy 0:05 +1` and `--json`
  `elapsed_s:5,queued:1` for a worker mid-`sleep` with a stacked task; `auto
  --json` returned `routed:"idle"` when reusing an idle worker and `auto
  --prefer-fresh` returned `routed:"spawn"` (a new callsign) with an idle worker
  present; `wait all` printed the `tt pi collect` hint while uncollected results
  existed, then went silent after `collect`. After respawning a worker onto the
  new extension, a task's `results/<id>.result` carried `started_at`+`ended_at`,
  the `--json` envelope showed `duration_s`, and `tt pi results` showed a `DUR`
  column (an older pre-timestamp record read `-`/`null`). `tt pi logs --lines N`
  dumped the worker's scrollback (TASK/nonce/WORKER_DONE present); its error
  paths (no callsign / unknown worker / bad `--lines`) all rejected correctly.
- **CosmosHub Anthropic routing (0.14.1), verified live 2026-07-14** against this
  repo's session: a fresh no-`--tier` worker launched
  `cosmoshub/qwen-3.7-max:max`, completed a tracked turn through `/v1/messages`,
  and reported tier `default`; process command and Pi footer verified routing.
  Removed-tier workers displayed `stale:<name>`, named dispatch refused them,
  and auto dispatch skipped them for the default worker. Testing also caught that Pi's
  Anthropic client appends `/v1/messages`, so its custom-provider base URL must
  be `https://api.cosmoshub.tech` rather than the OpenCode-style `/v1` base.
  Red/blue image probes showed Gemini 3.1 Pro and 3.5 Flash identify image
  content, while DeepSeek V4 Flash and Qwen 3.7 Max do not do so reliably;
  both Gemini models then returned `red` end-to-end through OpenCode, normal Pi,
  and Pi's private worker config. Capability declarations match those observed
  boundaries. A four-task, five-model worker benchmark selected Qwen 3.7 Max as
  the single default: 4/4 usable answers, approximately 280k Pi-reported tokens,
  and the best balance of source accuracy, pool reasoning, planning, edit
  quality, latency, and effective cost. Full evidence is in
  `docs/MODEL_DECISION.md`.

## Known limitations / not yet tested

- `tt down` reads a y/N confirmation from stdin — non-interactive callers must
  pipe `y`.
- `tt up`'s final attach fails harmlessly off a tty (expected headless).
- tmux-resurrect/continuum can race `tt up` and recreate a session with
  duplicate or shell-only `pi-*` windows. `tt up` heals this (dedups standard
  windows, revives dead REPLs), but keep `pi`/`claude` out of
  `@resurrect-processes` in `~/.tmux.conf` so stale REPL command lines are never
  resurrected.
- The `pi-worker:exclude-*` context filter is syntax/transpile-checked and
  exercised through a fake `before_agent_start` hook. Live turns have run with
  the extension loaded, but the final provider payload has not been inspected
  specifically to prove marked context was absent. Existing worker REPLs must
  be respawned to load extension changes.

## How to test

There is no harness — verify manually against a throwaway project. Use a real,
protocol-respecting task (do NOT ask the worker to "reply WORKER_DONE": that
makes it emit the marker WITHOUT the nonce footer, which is correctly rejected
as `interrupted`).

```sh
TD=$(mktemp -d /tmp/tt-test-XXXX); cd "$TD"
env -u TMUX tt up                       # builds dev/claude only; attach fails harmlessly off-tty
# lazy spawn on first send; task-id optional on wait
TID=$(tt pi send alfa - <<'P'
TASK: No code change needed — acknowledge receipt.
SUCCESS: acknowledged.
P
)
tt pi wait "$TID"                       # or: tt pi wait alfa
tt pi auto - <<<'TASK: ... ; SUCCESS: ...' ; tt pi wait all   # pick-for-me + fan-out join
STATE="${XDG_STATE_HOME:-$HOME/.local/state}/tt/$(tt name)"
tmux kill-session -t "=$(tt name)"; rm -rf "$TD" "$STATE"
```

For the queue/pool/--rm/--notify paths see the CHANGELOG (each was verified
live). Editing `pi-worker/extensions/tt-worker.ts` only takes effect on
a freshly launched REPL — respawn workers (`tt pi clear <cs>`) after changing
it. After syntax changes run `bash -n tt`. Live `pi` steps spend OpenAI Codex
quota — keep test tasks trivial.

## Worker pool

Complete (CHANGELOG has the increment history, DESIGN the rationale and
mechanics). Current behavior:

- **Lazy, no caste.** `tt up` pre-spawns nothing; workers (`alfa`…`zulu`) spawn
  on first `send`/`auto`, persist until removed, cap `min(cores-2, 26)`.
- **Dispatch.** `send <cs>` (named; run-next; lazy-spawns) · `auto` (pick idle →
  spawn → shared pool; echoes `using pi-<cs>`) · `auto --rm` (fresh ephemeral,
  reaped when idle) · `steer <cs|all>` (run-now injection) · `--notify`
  (fire-and-forget completion ping via the notify queue + lazy drainer).
- **Queues.** Per-worker `<cs>.queue/` (pinned) + shared `queue/` (stealable); an
  idle worker drains its own queue then steals from the pool (atomic-rename
  claim). `worker_state` keys `busy` off the `<cs>.busy` marker.
- **Wait.** `wait <cs|task-id|pool-id|all>`; task-id optional (defaults to the
  worker's latest); `all` joins every busy worker in one report.

Each increment was verified live against a throwaway project (see CHANGELOG).

## Pool & records caveats — not yet tested

- Extension (`tt-worker.ts`) changes take effect only on a respawned REPL, and
  only interruptions on the new REPL are resumable — keep that in mind after an
  upgrade (`tt pi clear <cs>` to respawn an existing worker).
- `tt pi collect` has **no stuck guard** (like a pool wait) — bound a possibly-
  wedged worker with `--timeout`.
- A `pool-<seq>` wait has **no stuck guard** — a pooled task legitimately waits
  for a worker to free up, so an unclaimable pool task (e.g. all workers dead)
  hangs until `--timeout`.
- The pool steal was verified with a **single** idle worker. The atomic-rename
  claim is written to be safe under multiple workers racing the same pool file,
  but that concurrent contention has not been exercised live.
- `tt pi auto`'s pool branch (all workers busy at the cap) was exercised by
  hand-dropping a pool task; saturating the real cap to trigger it organically
  was not (would spend a lot of quota).
- Ephemeral no-pool-steal: `TT_WORKER_EPHEMERAL` is set in `start_repl`'s launch
  env (code-verified) but the `/proc` read during the live test was permission-
  blocked, so the env reaching the REPL — and thus the pump skipping pool
  steals — was not directly confirmed. The reap itself was confirmed.

## Possible next steps

- Optional per-project config to auto-run the dev command.
- `tt pi logs --follow` (interval re-capture or a streamed transcript file) —
  the one-shot dump landed in 0.10.4; follow was deferred pending a streaming hook.

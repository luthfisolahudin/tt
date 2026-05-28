# tt — status & handoff

_Last updated: 2026-05-28 (v0.3.9)._

This is the "pick up where we left off" document. Read it before touching
`tt`.

## Current state

- `tt` v0.3.9, single bash file (`~/code/tt/tt`, symlinked from
  `~/.local/bin/tt`), plus one sidecar: `tt-worker.ts`.
- **`tt pi wait` waits forever by default** (2026-05-18). The user-facing
  completion wait now matches `tt x send`: no timeout unless
  `--timeout N` is provided, and `--timeout 0` is explicit forever.
  Internal health guards remain finite; in particular, an unconsumed
  trigger still fails fast after 20 s because that indicates stuck
  plumbing, not a long model turn.
- **`tt x observe` samples Claude panes for classifier data** (2026-05-18;
  SQLite storage updated 2026-05-26).
  Passive diagnostics command: `tt x observe [run] [--interval N]
  [--duration N] [--all]`.
  Bare `tt x observe` aliases to `tt x observe run`. It reuses the same
  classifier as `tt x send`, captures plain and escaped pane tails, writes to
  the global tt SQLite database
  `${XDG_STATE_HOME:-$HOME/.local/state}/tt/x-observe.sqlite` by default,
  and ignores duplicate non-`ts` payloads at insert time.
  `scripts/import-x-observe-jsonl.sh` imports the old JSONL log and leaves the
  source file in place. It never sends keys or takes `x-send.lock`; it logs
  pane text intentionally and prints a startup warning.
- **`tt x send` waits for safe Claude Code input** (2026-05-18, v0.3.9).
  Cross-session delivery now serializes per target with
  `<target-state>/x-send.lock`, rejects unsafe plain-capture states
  (`esc interrupt` / Ctrl-C cancel hints), treats queued-message banners and
  collapsed queued-message `paste again to expand` hints as safe, then uses
  escaped capture to
  classify the current bottom `❯` prompt. Empty prompts are safe; visible
  text after `❯` is a real user draft and waits; explicitly dim (`ESC[2m`)
  suggestion text is safe because paste replaces Claude Code's suggestion.
  Cursor-highlighted suggestions where the first character is reverse-video
  (`ESC[7m`) and the rest is dim (`ESC[0;2m`) are also safe.
  Missing bottom prompt also waits, covering plan
  confirmation, question, or in-flight states. The wait is infinite by
  default and Ctrl-C cancels; `--timeout N` fails instead of waiting
  forever.
- **`tt up` no longer black-screens during boot** (2026-05-16, v0.3.6).
  Async startup (v0.3.5) made `tt up` attach instantly — but the user
  then watched the `claude` window stay black until the workers were
  ready. Cause: claude's TUI clears the screen on launch, and its first
  paint was CPU-starved behind three concurrent pi `node` startups; the
  old order even spawned the pi REPLs *before* `auto_launch_claude`.
  Fixes: `up_cmd` now runs `auto_launch_claude` *before* `ensure_pi_repls`
  (pi-spawning split out of `ensure_standard_windows`), and `start_repl`
  launches pi under `nice -n 19` (+ `ionice -c3` where available) so the
  interactive claude TUI keeps scheduler priority. pi workers are
  API-I/O bound, so the low priority costs them little.
- **`tt up` is now instant — async REPL startup** (2026-05-16, v0.3.5).
  `tt up` previously revived the three immortal pi REPLs serially, each
  `launch_repl` blocking up to 40 s on a `pgrep` + `<cs>.ready`
  handshake — so a cold `tt up` paid `3 ×` pi-boot time before the user
  could do anything. The blocking is gone: `launch_repl` is split into
  `start_repl` (non-blocking `respawn-pane`, stamps `<cs>.starting`) and
  `wait_repl_ready` (the poll loop). `tt up` fires `start_repl` for
  every immortal, launches claude, and attaches at once; the REPLs boot
  in the background. The 40 s wait moved to `tt pi send` via
  `ensure_repl_ready` — lazy, per-target-worker, and hidden behind the
  user's think-time before the first delegation. New `starting` worker
  state covers a REPL still inside its boot window.
- **Control-channel hardening** (2026-05-16). The trigger is now
  consumed by **rename** (`<cs>.trigger` → `<cs>.trigger.consuming`),
  not read-then-truncate, so a concurrent tt write is never clobbered.
  `<cs>.result` became a lifecycle file (`running`→`done`/`blocked`/
  `other`/`error`): the extension writes `running` the instant it
  consumes a trigger, so `tt pi wait` can tell "not picked up" from
  "in progress" and **fast-fails after 20 s** on an unconsumed trigger
  instead of burning the full timeout. Extension exceptions surface as
  `status: error` (or `<cs>.log` when there is no result yet) rather
  than a silent hang. tt reads the result in a single snapshot to avoid
  torn reads. `clear` appends a `{"clear":<gen>}` marker to
  `tasks.jsonl` instead of truncating it, so task ids never recur
  across generations.
- **State dir moved to XDG** (2026-05-16). State now lives under
  `${XDG_STATE_HOME:-$HOME/.local/state}/tt/<session>/` instead of
  `/tmp/tt/`. State (task logs, pi session-dirs) survives reboots.
  Override with `TT_STATE_DIR`.
- **Worker pi-agent split** (2026-05-28). `tt` workers launch with
  `PI_CODING_AGENT_DIR=${XDG_DATA_HOME:-$HOME/.local/share}/tt/pi-agent`
  (override with `TT_PI_AGENT_DIR`) so normal `pi` sessions keep using
  the user's `~/.pi/agent` and do not load `tt-worker.ts`. The worker
  agent dir contains `settings.json` and `APPEND_SYSTEM.md`; worker REPLs
  also pass `--no-skills` so a delegate never loads the delegating skill.
- **XDG data install** (2026-05-16, updated 2026-05-28). `~/.local/share/tt/`
  holds runtime worker data plus symlinks to repo-owned source files. In
  particular, `~/.local/share/tt/pi-agent/` is a real writable runtime
  directory (not a symlink to the git checkout): `tt-worker.ts` and
  repo-owned `pi-agent/` files are symlinked in, except `settings.json`
  which is copied because pi mutates it with changelog metadata. pi can
  write `auth.json` or changelog metadata there without dirtying the repo.
  Global skill links point through `~/.local/share/tt/` — moving the repo requires only
  updating those symlinks, not hunting scattered hardcoded paths.
- **Global APPEND_SYSTEM.md auto-injected** (2026-05-16, updated 2026-05-28).
  If the project has no `.pi/APPEND_SYSTEM.md`, `launch_repl` passes
  `--append-system-prompt` pointing at the worker file in
  `~/.local/share/tt/pi-agent/APPEND_SYSTEM.md`. The project directory is
  never touched; a project-local `.pi/APPEND_SYSTEM.md` takes precedence
  naturally.
- **`tt up` auto-launches claude** (2026-05-16). If the `claude` pane
  is running a bare shell, `tt up` sends
  `claude --continue --allow-dangerously-skip-permissions` into it.
  Resumes the last conversation on re-attach after a crash or reboot.
- **`tt up` always focuses the claude window** (2026-05-16).
  `enter_session` now accepts an optional window target;
  `up_cmd` passes `"claude"` so focus always lands on the orchestrator
  window regardless of which tmux session `tt up` is called from.
- **The pi-worker model was rewritten** (2026-05-16). The old `pi -p`
  one-shot + pane-watermark mechanism is gone. Each `pi-*` window now
  hosts a **live interactive pi REPL**; `tt` drives it through the
  `tt-worker.ts` pi extension over plain files. See `docs/DESIGN.md`.
- **Tier switching is now a runtime operation** (2026-05-16). `tt pi
  send --low/--medium` no longer respawns the REPL — the tier travels
  in the trigger and `tt-worker.ts` applies it via
  `pi.setThinkingLevel`. pi context is preserved across a tier change.
- **Robust task-completion detection** (2026-05-16). Three approaches
  combined:
  - **Nonce (approach 2)**: `pi_send` generates a random 16-char hex
    nonce per dispatch, injects `nonce=<N>` into the WORKER_DONE notes
    field, writes it into the trigger header. `tt-worker.ts` requires
    the nonce to appear in the terminal block before setting
    `status=done`. Stale markers from prior context are ignored.
  - **Terminal-position (approach 3)**: `tt-worker.ts` only classifies
    `done` when the WORKER_DONE block is the last thing in the response
    (only `field: value` lines after it, then whitespace). Embedded
    or mid-response WORKER_DONE markers are ignored.
  - **Quarantine (approach 4)**: `worker_state` returns `interrupted`
    when a task's result is `status=other`. `pi_send` refuses to
    dispatch to an interrupted worker; `tt pi clear` is required first.
    `tt pi status` displays the `interrupted` state.

## The rewrite — what changed

- `tt-worker.ts` — pi extension. Watches `<cs>.trigger` (prompt in),
  consumes it by rename, writes a `running` result, then a terminal
  result on `agent_end` (result out), touches `<cs>.ready`, logs
  failures to `<cs>.log`. Inert unless `TT_WORKER_CS` is set. Trigger
  line 1 is `<id> <tier> <nonce>`; the extension applies the tier with
  `pi.setThinkingLevel` and stores the nonce for completion validation
  before sending the turn.
- `pi-agent/settings.json` — worker-only pi config used via
  `PI_CODING_AGENT_DIR`; installs `tt-worker.ts` via `extensions` and
  excludes `delegating-to-pi` via `skills`. `tt` also starts worker REPLs
  with `--no-skills` so workers cannot become orchestrators through a
  project- or user-discovered skill. Normal `~/.pi/agent/settings.json`
  is user-owned and no longer carries tt worker resources.
- `tt` — `spawn_pi_window` launches a REPL; `launch_repl`/`ensure_repl`/
  `repl_running` manage it; `pi_send`/`pi_wait`/`pi_clear` use the
  trigger/result files; all `capture-pane`/watermark code deleted.
  `pi_send` no longer respawns on a tier change — it only writes the
  tier into the trigger and the `.tier` file.

## Verification — full retest (2026-05-16, v0.3.2)

Run against `tt-fbba` (the tt repo's own session), kept alive afterwards.

1. **`tt up`** — `dev claude pi-alfa pi-bravo pi-charlie`; all three
   REPLs idle. ✅
2. **`tt pi status`** — idle/busy/blocked/interrupted/down/missing rows. ✅
3. **Normal completion** — nonce as first field in WORKER_DONE block,
   exit 0. ✅
4. **Persistent turn** — second task on same worker recalls prior context. ✅
5. **Stale WORKER_DONE in code fence** — terminal-position check ignores
   fenced example; real terminal block classified `done`. ✅
6. **BLOCKED path** — contradictory task → `BLOCKED` block with nonce +
   reason fields, `wait` exit 0. ✅
7. **Interrupted quarantine (tmux Escape)** — Escape sent to pi pane
   mid-task; worker landed `interrupted`; `send` refused; `clear`
   recovered. ✅
8. **Interrupted quarantine (injected `status=other`)** — same flow via
   direct file injection. ✅
9. **Runtime tier switch** — `send alfa --medium` after `--low`; no
   respawn; codeword recalled across tier boundary. ✅
10. **`tt pi add` / cap** — spawns `delta`, `echo`; third add refused. ✅
11. **`tt pi rm` (alias `remove`) / `popidle`** — removes non-immortal; `popidle` drops
    highest-NATO idle non-immortal. ✅

## Verification — control-channel hardening (2026-05-16, v0.3.3)

- `bash -n tt` passes; `tt-worker.ts` passes `bun --check`.
- Happy path — `clear charlie` (loads the new extension) → trivial
  read-only task → `<cs>.result` transits `running` → `done`, `wait`
  exits 0, no `<cs>.log`. ✅
- Task-id uniqueness — after `clear`, `tasks.jsonl` keeps the old turn
  line plus a `{"clear":3}` marker; the next `send` is `charlie-3`,
  not a recurring `charlie-1`. ✅
- The 20 s fast-fail on an unconsumed trigger and the `status: error`
  channel are now exercised live — see "closed test gaps" below.

## Verification — session lifecycle (2026-05-16, v0.3.4)

Run against throwaway `/tmp/tt-test-*` projects.

- Cold `tt up` — session + `dev`/`claude`/`pi-{alfa,bravo,charlie}`. ✅
- `tt up` heals — kill `pi-bravo`, `tt up` recreates it; a second
  `tt up` on a healthy session is a no-op (no duplicates). ✅
- `send`/`wait` — `<cs>.result` transits `running`→`done`. ✅
- `clear` marker + id uniqueness — next id is `alfa-3`, not `alfa-1`. ✅
- `tt pi clear` does not orphan the old REPL (`respawn-pane -k` reaps
  the whole process group). ✅
- `tt down` — completes, session + state dir removed; with 3 live
  REPLs (9 processes incl. pi grandchildren) it leaves 0 survivors. ✅

## Verification — closed test gaps (2026-05-16, v0.3.6)

Run against a throwaway `/tmp/tt-test-*` project; three mechanisms that
had only been code-reviewed are now exercised live.

- **20 s fast-fail on an unconsumed trigger** — the worker's grandchild
  `pi` REPL process was `kill -STOP`ed (bash+node parents stay alive, so
  `repl_running` still matches), then a task was dispatched. `tt pi wait`
  fast-failed at **exactly 20 s**, exit 1, with `pi-alfa never consumed
  the trigger for alfa-2 (worker stuck or trigger watch dead)` — not the
  full timeout. `kill -CONT` resumed the REPL and it consumed the
  pending trigger normally. ✅
- **`status: error` channel** — `id/status: error/---/<text>` injected
  into `<cs>.result`; `tt pi wait` printed the `---` body to stderr and
  died with `pi-bravo reported an internal error for bravo-1`, exit 1
  (`tt:518-520`). The extension-side writes (`tt-worker.ts:127-134,
  184-186`) remain code-reviewed only. ✅
- **Persistent multi-turn chain (4 turns)** — `charlie` ran turns 1–4:
  store codeword `orbit` → append `-7` (`orbit-7`) → reverse (`7-tibro`)
  → recall the original. Turn 4 correctly returned `orbit`, proving
  context held across 4 turns with no respawn (`gen` stayed `g0`,
  `tasks.jsonl` held ids `charlie-1..4`). ✅

## Bugs found & fixed

- **`tt down` aborted mid-teardown** (v0.3.4) — `down_cmd`'s
  `pid=$(pgrep … | head -1)` aborted under `set -euo pipefail`: pgrep
  exits non-zero on no match, and `head` closing the pipe SIGPIPEs
  pgrep even on a match. `tt down` died before `kill-session`/`rm`,
  leaving a half-torn-down session. Now `down_cmd` SIGTERMs each pi
  window's whole process group via the pane pid — no pgrep pipeline,
  and no orphaned pi grandchild.
- **`tt up` was not idempotent** (v0.3.4) — it built standard windows
  only via `create_session` on a cold session, so it could not heal a
  session that existed but lost windows (the state `tt down`'s abort
  left behind). Added `ensure_standard_windows`, called on every
  `tt up`.
- **tmux-resurrect/continuum vs tt** (v0.3.4) — a continuum auto-restore
  recreates the session concurrently with `tt up`, leaving duplicate
  `pi-*` windows (ambiguous `tmux -t` targets → "can't find window"),
  and restored pi windows hold a bare shell with no REPL. Fixes:
  `ensure_standard_windows` now collapses duplicate standard windows
  (`dedup_windows`) and revives a dead REPL in an existing pi window
  (`ensure_repl`), not just missing windows. Also dropped `pi`/`claude`
  from `@resurrect-processes` in `~/.tmux.conf` so stale REPL command
  lines are never resurrected.

- **`pane_current_command` is unreliable** — pi runs as a grandchild
  (`bash → node → pi`); `repl_running` matches the live pi process by
  its unique `--session-dir` path with `pgrep -f` instead.
- **Startup trigger race** — the extension truncated `<cs>.trigger` on
  `session_start`. Fixed with create-if-missing only, plus the
  `<cs>.ready` handshake `launch_repl` waits on.
- **BLOCKED masked by WORKER_DONE** — `tt-worker.ts` classifies
  `BLOCKED` ahead of `WORKER_DONE`.
- **`--medium` tier switch wedged the worker** (found in this retest,
  step 6 of the old plan). The tier-change path called `launch_repl`,
  which `rm -f`s `<cs>.result`; with `<cs>.tasks.jsonl` still populated,
  `worker_state` reported a false `busy` and `send` aborted — leaving
  the worker stuck until `clear`. **Fix:** tier switching no longer
  respawns the REPL at all — it is a runtime `setThinkingLevel` call
  (see "Tier switching is now a runtime operation" above). The respawn,
  and therefore the stale-file bug, is gone.
- **`tmux automatic-rename` corrupted pi window names** — tmux's
  automatic-rename fired between `new-window` and `respawn-pane` in
  `launch_repl`, renaming `pi-charlie` away from its assigned name and
  causing "can't find window: pi-charlie" on `tt up`. Fixed by calling
  `set-window-option automatic-rename off` in `spawn_pi_window`
  immediately after `new-window`.
- **`kill-window` races in `down_cmd`, `pi_rm_cmd`, `pi_popidle_cmd`**
  — a pi window could disappear between the `window_exists` guard and
  the `tmux kill-window` call, producing a spurious "can't find window"
  error. Fixed with `2>/dev/null || true` on all three kill-window calls.

## Verification — session discovery (2026-05-17, v0.3.8)

New verbs `tt x list [--all]` / `tt x ls [--all]` enumerate tt sessions
available to message. `tt up` now writes `$PWD` to `$(state_dir)/project`
so each session's working directory is recoverable without visiting it.

- **`tt x list`** — reads `~/.local/state/tt/*/`, cross-checks each subdir
  with `tmux has-session`, then tests the `claude` window's foreground
  command. Prints only `ready` sessions (orchestrator running) with their
  path. ✅
- **`tt x ls --all`** — same sweep with a STATUS column: `ready` /
  `no-orchestrator` / `down` (state dir exists but no tmux session). ✅
- **Empty case** — no sessions matching the filter prints a clear
  `(no tt sessions …)` message, exit 0. ✅
- **`project` file** — `tt up` writes it unconditionally (new and
  existing sessions); `tt x list` falls back to `-` if absent (pre-v0.3.8
  state dirs). ✅

## Verification — cross-session messaging (2026-05-16, v0.3.7)

New verb `tt x send [--timeout N] <session-id> (FILE|-)` delivers a
message to another tt session's orchestrator via tmux bracketed paste +
`sleep 0.3` + Enter, after waiting for empty Claude Code input. See
DESIGN.md "Cross-session messaging". Original staged test, all PASS:

- **Guard paths** — non-existent session, missing args, missing source,
  unreadable file, unknown `x` subcommand, session with no `claude`
  window, `claude` window at a bare shell, empty message: each rejected
  with a clear `tt: x send: …` error, exit 1.
- **Delivery mechanics** (against a plain `cat` target): single-line,
  3-line multiline, shell metacharacters (`` ` `` `$()` `"` `'` `\` `|`
  `;`), leading newlines, 4 KB body, FILE and `-` (stdin) sources — all
  arrive byte-intact with the `[tt x from <sender>]` header. `$(cat)`
  strips the *trailing* newline (harmless; Enter re-adds one).
- **Live orchestrator** (Sonnet 4.6, medium): a 2-line message landed as
  exactly one user turn, header preserved, submitted cleanly — the
  `sleep 0.3` was sufficient. Two rapid back-to-back sends arrived as two
  distinct turns, no clobber/interleave (per-process `tt-x-$$` buffers).

## Known limitations / not yet tested

- `tt down` reads a y/N confirmation from stdin — a non-interactive
  caller must pipe `y`. Not exercised in the latest retest (it would
  kill the session the user is attached to).
- `tt up`'s final `attach` fails harmlessly when run off a tty
  (`open terminal failed: not a terminal`, exit 1) — expected headless.

## How to test again

```sh
TD=$(mktemp -d /tmp/tt-test-XXXX); cd "$TD"
# No need to copy APPEND_SYSTEM.md — tt up injects pi-agent/APPEND_SYSTEM.md via --append-system-prompt
env -u TMUX tt up                       # attach fails harmlessly off-tty
TID=$(tt pi send alfa <(printf 'TASK: reply WORKER_DONE\nSUCCESS: done\n'))
tt pi wait alfa "$TID"
STATE="${XDG_STATE_HOME:-$HOME/.local/state}/tt/$(tt name)"
tmux kill-session -t "=$(tt name)"; rm -rf "$TD" "$STATE"
```

Editing `tt-worker.ts` only takes effect on a freshly launched REPL —
respawn workers (`tt pi clear <cs>`) after changing the extension.
Live pi steps spend OpenAI Codex quota — keep test tasks trivial.

## Possible next steps

- A `tt pi logs <cs>` verb could dump a worker's REPL scrollback.
- Per-project optional config to auto-run the dev command.

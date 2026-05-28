# Changelog

Notable changes to `tt`, newest first. Reconstructed from git history and prior
`docs/STATUS.md` notes. There are no release tags; versions follow the `VERSION`
constant in `tt` and the commit-message milestones (the constant jumped
0.3.0 → 0.3.4, but 0.3.1–0.3.3 were tracked as distinct milestones).

## [0.4.0] — 2026-05-29

- **Session version stamping.** `tt up` writes the running tt version to the
  tmux session env as `TT_VERSION` and to `$(state_dir)/version`; newly spawned
  worker REPLs also receive `TT_VERSION`.
- **Worker pi-worker split** (2026-05-28). Workers launch with
  `PI_CODING_AGENT_DIR=${XDG_DATA_HOME:-$HOME/.local/share}/tt/pi-worker`
  (override `TT_PI_WORKER_DIR`; legacy `TT_PI_AGENT_DIR` still honored) so normal
  `pi` sessions keep using `~/.pi/agent` and never load `tt-worker.ts`. The
  worker runtime dir is real and writable, lazily filled missing-only.

## [0.3.9] — 2026-05-18

- **`tt x send` waits for safe Claude Code input.** Serializes per target with
  `x-send.lock`, rejects unsafe/queued UI states, and classifies the bottom
  prompt (incl. highlighted suggestions) before pasting.
- **`tt pi wait` waits forever by default**; `--timeout N` bounds it
  (`--timeout 0` is explicit forever). Internal health guards stay finite.
- **`tt x observe`** added — passive, read-only classifier-tuning loop; events
  stored in the global `x-observe.sqlite`, deduped on payload (2026-05-26).

## [0.3.8] — 2026-05-17

- **Session discovery** — `tt x list` / `tt x ls [--all]` enumerate tt sessions
  available to message; `tt up` writes `$PWD` to `$(state_dir)/project`.
- `tt pi remove` alias for `tt pi rm`; fall back to a fresh `claude` when
  `--continue` finds no conversation.

## [0.3.7] — 2026-05-16

- **Cross-session messaging** — `tt x send <session-id> (FILE|-)` delivers a
  message into another tt session's orchestrator via tmux bracketed paste.
- Renamed `tt pi down` → `tt pi rm`; self-correcting error when a `send` source
  is not a file; `delegating-to-pi` skill recalibrated to delegate-first.

## [0.3.6] — 2026-05-16

- **`tt up` no longer black-screens during boot.** `up_cmd` runs
  `auto_launch_claude` before `ensure_pi_repls`, and `start_repl` launches pi
  under `nice -n 19` (+ `ionice -c3` where available) so the claude TUI keeps
  priority.

## [0.3.5] — 2026-05-16

- **Async REPL startup.** `tt up` is instant: it fires `start_repl` for every
  immortal and attaches at once; the 40 s readiness wait moved to `tt pi send`
  via `ensure_repl_ready` (lazy, per-target). New `starting` worker state.

## [0.3.4] — 2026-05-16

- **`tt up` is idempotent and heals** missing/dead/duplicate windows
  (`ensure_standard_windows`, `dedup_windows`, `ensure_repl`).
- Fixed: `tt down` aborting mid-teardown under `set -euo pipefail` (now SIGTERMs
  the whole pi process group); duplicate `pi-*` windows from a
  tmux-resurrect/continuum race; kill-window races.

## [0.3.3] — 2026-05-16

- **Control-channel hardening.** The trigger is consumed by rename
  (`<cs>.trigger` → `<cs>.trigger.consuming`), not read-then-truncate.
  `<cs>.result` became a lifecycle file (`running`→`done`/`blocked`/`other`/
  `error`), enabling the 20 s fast-fail on an unconsumed trigger. `clear`
  appends a `{"clear":<gen>}` marker so task ids never recur.

## [0.3.2] — 2026-05-16

- **Nonce promoted to a dedicated field** in the `WORKER_DONE` / `BLOCKED`
  blocks; the live-REPL model was comprehensively retested at this milestone.

## [0.3.1] — 2026-05-16

- **Robust task-completion detection** — random per-dispatch nonce validation,
  terminal-position check (`WORKER_DONE` must be last), and `interrupted`
  quarantine (`status=other`) guarding against stale/embedded markers.

## [0.3.0] — 2026-05-16

- **State dir moved to XDG** — `${XDG_STATE_HOME:-$HOME/.local/state}/tt/`
  (override `TT_STATE_DIR`); state survives reboots.
- **`tt up` auto-launches claude** (`claude --continue …`) when the `claude`
  pane is a bare shell.
- **Global `APPEND_SYSTEM.md` auto-injected** unless the cwd has its own
  `.pi/APPEND_SYSTEM.md`.

## [0.2.0] — 2026-05-16

- **Runtime tier switching** — `send --low/--medium` applies the tier via
  `pi.setThinkingLevel` without respawning; pi context is preserved.
- **`delegating-to-pi` made a global skill** and kept out of pi workers
  (`--no-skills` + `pi-worker/settings.json`) so a delegate can't become the
  orchestrator.

## [0.1.0] — 2026-05-16

- **Initial release.** Per-project tmux session (`dev`, `claude`,
  `pi-{alfa,bravo,charlie}` windows) plus an on-demand pi worker pool (cap 5);
  `tt up`/`down`/`attach`/`name` and
  `tt pi send`/`wait`/`status`/`clear`/`add`/`rm`/`popidle`; the
  `delegating-to-pi` consumer skill.
- **Live pi REPL model** — superseded the initial `pi -p` one-shot +
  pane-watermark prototype: each `pi-*` window now hosts a persistent
  interactive pi REPL driven through the `tt-worker.ts` extension over plain
  trigger/result files.

# tt — status & handoff

_Last updated: 2026-05-16._

This is the "pick up where we left off" document. Read it before touching
`tt`.

## Current state

- `tt` v0.1.0, single bash file (`~/code/tt/tt`, symlinked from
  `~/.local/bin/tt`), plus one sidecar: `tt-worker.ts`.
- **The pi-worker model was rewritten** (2026-05-16). The old `pi -p`
  one-shot + pane-watermark mechanism is gone. Each `pi-*` window now
  hosts a **live interactive pi REPL**; `tt` drives it through the
  `tt-worker.ts` pi extension over plain files. See `docs/DESIGN.md`.

## The rewrite — what changed

- `tt-worker.ts` — new pi extension. Watches `<cs>.trigger` (prompt in),
  writes `<cs>.result` on `agent_end` (result out), touches `<cs>.ready`.
  Inert unless `TT_WORKER_CS` is set.
- `~/.pi/agent/settings.json` — installs `tt-worker.ts` globally via
  `extensions`, and excludes the `delegating-to-pi` skill via `skills`.
- `tt` — `spawn_pi_window` launches a REPL; `launch_repl`/`ensure_repl`/
  `repl_running` manage it; `pi_send`/`pi_wait`/`pi_clear` use the
  trigger/result files; all `capture-pane`/watermark code deleted.

## Verification — what was tested (2026-05-16)

Run against `tt-fbba` (the tt repo itself), torn down afterwards.

1. **`tt up`** — creates `dev claude pi-alfa pi-bravo pi-charlie`; all
   three REPLs launch with `tt-worker.ts` loaded. ✅
2. **`tt pi status`** — idle/busy/blocked/down/missing rows. ✅
3. **`send` + `wait`** — `send alfa` → task-id `alfa-1`; `wait` returned
   the `WORKER_DONE` block, exit 0. ✅
4. **Persistent turn** — second turn `alfa-2` on the same worker. ✅
5. **`clear`** — bumps gen g0→g1, respawns the REPL, resets task log;
   next `send` starts at turn 1 on the new session-dir. ✅
6. **Parallel** — `alfa` and `bravo` each ran a turn. ✅
7. **BLOCKED path** — impossible task → result `status: blocked`,
   `wait` surfaced the `BLOCKED:` line. ✅
8. **Human coexistence** — typing into a `pi-*` window by hand works;
   the extension records the human turn with id `-`, so tt's `wait` is
   unaffected. (Verified during the design experiments.) ✅

## Bugs found & fixed during the rewrite

- **`pane_current_command` is unreliable** — pi runs as a grandchild
  (`bash → node → pi`); tmux reported `bash`/`node`/`pi`
  inconsistently, so REPL-liveness detection flapped. **Fix:**
  `repl_running` matches the live pi process by its unique
  `--session-dir` path with `pgrep -f`.
- **Startup trigger race** — the extension truncated `<cs>.trigger` on
  `session_start`, which could clobber a trigger tt wrote during launch.
  **Fix:** create-if-missing only, plus the `<cs>.ready` handshake that
  `launch_repl` waits on.
- **BLOCKED masked by WORKER_DONE** — pi sometimes emits a `BLOCKED:`
  line *and* a `WORKER_DONE` wrapper. **Fix:** `tt-worker.ts` classifies
  `BLOCKED` ahead of `WORKER_DONE`.

## Known limitations / not yet tested

- **`--medium` tier switch** — code path respawns the REPL on tier
  change; not exercised end-to-end since the rewrite.
- **Long persistent chains** (>2 turns) not exercised since the rewrite.
- `tt down` reads a y/N confirmation from stdin — a non-interactive
  caller must pipe `y`.
- `tt up`'s final `attach` fails harmlessly when run off a tty
  (`open terminal failed: not a terminal`, exit 1) — expected headless.

## How to test again

```sh
TD=$(mktemp -d /tmp/tt-test-XXXX); cd "$TD"
mkdir .pi && cp ~/code/tt/.pi/APPEND_SYSTEM.md .pi/ 2>/dev/null || true
env -u TMUX tt up                       # attach fails harmlessly off-tty
TID=$(tt pi send alfa <(printf 'TASK: reply WORKER_DONE\nSUCCESS: done\n'))
tt pi wait alfa "$TID"
tmux kill-session -t "=$(tt name)"; rm -rf "$TD" "/tmp/tt/$(tt name)"
```

Live pi steps spend OpenAI Codex quota — keep test tasks trivial.

## Possible next steps

- Exercise the `--medium` respawn path and longer persistent chains.
- A `tt pi logs <cs>` verb could dump a worker's REPL scrollback.
- Per-project optional config to auto-run the dev command.

# tt — status & handoff

_Last updated: 2026-05-16 (v0.3.1)._

This is the "pick up where we left off" document. Read it before touching
`tt`.

## Current state

- `tt` v0.3.1, single bash file (`~/code/tt/tt`, symlinked from
  `~/.local/bin/tt`), plus one sidecar: `tt-worker.ts`.
- **State dir moved to XDG** (2026-05-16). State now lives under
  `${XDG_STATE_HOME:-$HOME/.local/state}/tt/<session>/` instead of
  `/tmp/tt/`. State (task logs, pi session-dirs) survives reboots.
  Override with `TT_STATE_DIR`.
- **XDG data install** (2026-05-16). `~/.local/share/tt/` holds
  symlinks to the tt repo's `.pi/`, `.agents/`, and `tt-worker.ts`.
  Global skill links and `~/.pi/agent/settings.json` now point through
  `~/.local/share/tt/` — moving the repo requires only updating those
  symlinks, not hunting scattered hardcoded paths.
- **Global APPEND_SYSTEM.md auto-injected** (2026-05-16). If the
  project has no `.pi/APPEND_SYSTEM.md`, `launch_repl` passes
  `--append-system-prompt` pointing at the global file in
  `~/.local/share/tt/.pi/`. The project directory is never touched;
  a project-local `.pi/APPEND_SYSTEM.md` takes precedence naturally.
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
  writes `<cs>.result` on `agent_end` (result out), touches `<cs>.ready`.
  Inert unless `TT_WORKER_CS` is set. Trigger line 1 is `<id> <tier> <nonce>`;
  the extension applies the tier with `pi.setThinkingLevel` and stores
  the nonce for completion validation before sending the turn.
- `~/.pi/agent/settings.json` — installs `tt-worker.ts` globally via
  `extensions`, and excludes the `delegating-to-pi` skill via `skills`.
  The skill is also symlinked into `~/.agents/skills/` + `~/.claude/skills/`
  so Claude sees it globally; pi scopes skill `!`-excludes to the
  discovery location, so the exclude is repeated in this repo's
  `.pi/settings.json` to cover the project-discovered copy under
  `.agents/skills/`.
- `tt` — `spawn_pi_window` launches a REPL; `launch_repl`/`ensure_repl`/
  `repl_running` manage it; `pi_send`/`pi_wait`/`pi_clear` use the
  trigger/result files; all `capture-pane`/watermark code deleted.
  `pi_send` no longer respawns on a tier change — it only writes the
  tier into the trigger and the `.tier` file.

## Verification — full retest (2026-05-16)

Run against `tt-fbba` (the tt repo's own session), kept alive afterwards.

1. **`tt up`** — `dev claude pi-alfa pi-bravo pi-charlie`; all three
   REPLs idle. ✅
2. **`tt pi status`** — idle/busy/blocked/down/missing rows. ✅
3. **`send` + `wait`** — `bravo-1`, returned `WORKER_DONE`, exit 0. ✅
4. **Persistent turn** — `bravo-2` on the same worker, same
   session-dir. ✅
5. **Long persistent chain** — `bravo-3/4/5`; turn numbering stays
   monotonic, session-dir unchanged across all 5 turns. ✅
6. **`clear`** — bumps gen, respawns the REPL on a new session-dir,
   resets the task log; next `send` starts at turn 1. ✅
7. **Parallel** — `alfa`, `bravo`, `charlie` each ran a turn
   independently. ✅
8. **BLOCKED path** — contradictory task → result `status: blocked`,
   `wait` surfaced the `BLOCKED:` line, exit 0. ✅
9. **`tt pi add` / cap** — spawns `delta` then `echo`; a third `add`
   is refused at the cap of 5. ✅
10. **`tt pi down` / `popidle`** — removes a non-immortal worker;
    immortals are refused; `popidle` drops the highest-NATO idle
    non-immortal. ✅
11. **Runtime tier switch** — `send alfa --medium` after a `--low`
    turn: the pi process did **not** respawn, the turn completed
    `WORKER_DONE` exit 0, `alfa.tier` became `medium`, `alfa.result`
    was never deleted. Back-to-back `--medium` also did not respawn. ✅
12. **Context preserved across a tier switch** — turn 1 (`--low`) noted
    a codeword; turn 2 (`--medium`) recalled it correctly, proving the
    pi session survived the tier change. ✅

## Bugs found & fixed

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

## Known limitations / not yet tested

- `tt down` reads a y/N confirmation from stdin — a non-interactive
  caller must pipe `y`. Not exercised in the latest retest (it would
  kill the session the user is attached to).
- `tt up`'s final `attach` fails harmlessly when run off a tty
  (`open terminal failed: not a terminal`, exit 1) — expected headless.

## How to test again

```sh
TD=$(mktemp -d /tmp/tt-test-XXXX); cd "$TD"
# No need to copy APPEND_SYSTEM.md — tt up injects global one via --append-system-prompt
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

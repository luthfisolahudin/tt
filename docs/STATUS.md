# tt — status & handoff

_Last updated: 2026-05-16._

This is the "pick up where we left off" document. Read it before touching
`tt`.

## Current state

- `tt` v0.1.0, ~552 lines of bash, single file. Lives at `~/code/tt/tt`,
  symlinked from `~/.local/bin/tt`.
- **All 11 verification steps pass** (see below).
- Consumers in `bassaudio-storefront` are wired up:
  `.agents/skills/delegating-to-pi/SKILL.md` and `AGENTS.md` / `CLAUDE.md`.

## Verification — what was tested (2026-05-16)

Run in a throwaway project dir (`/tmp/tt-test-*`) with a copy of
`bassaudio-storefront/.pi/APPEND_SYSTEM.md` so pi knew the Worker Mode
protocol. Test session torn down afterwards.

Structural (no Codex quota):

1. **Cold start** — `tt up` creates the session with windows
   `dev claude pi-alfa pi-bravo pi-charlie`. ✅
2. **Idempotency** — second `tt up` adds no duplicate windows. ✅
3. **Help** — `tt --help` is 108 lines, comprehensive. `--version`,
   `name` work. ✅
4. **Add / cap / popidle** — `add`→delta→echo, third errors at cap 5;
   `popidle` removes echo then delta then no-ops. ✅
5. **Down semantics** — `down delta` (idle) kills it; `down alfa`
   refuses (immortal). ✅
6. **Clear** — `clear bravo` bumps generation g0→g1. ✅
7. **Status** — correct state/tier/gen rows. ✅
8. **Cross-project isolation** — different `$PWD` → different session
   name. ✅

Live pi (Codex quota spent):

9. **Ephemeral round-trip** — `send` alfa a trivial file-creation task,
   `wait` returns the `WORKER_DONE` block; file created with exact
   content. ✅
10. **Task-ID accuracy** — back-to-back persistent turns A then B on the
    same worker; `wait` for turn B returned B's `WORKER_DONE`
    (`files_changed: pi-test-b.txt`), NOT A's stale marker — proves the
    watermark anchors correctly. ✅
11. **Tier switch** — `send bravo --medium` recorded `"tier":"medium"`
    in `tasks.jsonl`, wrote `bravo.tier`, and the pane command used
    `gpt-5.5:medium`. ✅

## Bugs found & fixed during verification

The script as first written had four real bugs; all are fixed in the
current `tt`. Do not regress them.

1. **`set-option` rejected the `=` target prefix.** `create_session` ran
   `tmux set-option -t "=$s" history-limit ...`, which fails with "no
   such session" — `set-option` does not accept the `=` exact-match
   prefix that other tmux subcommands do. Result: only the `dev` window
   was ever created. **Fix:** use the bare-name + colon form `"$s:"`.

2. **`tmux attach` cannot nest.** `up`/`attach` ran `tmux attach` even
   when already inside another tmux session, which errors. **Fix:** new
   `enter_session()` helper uses `switch-client` when `$TMUX` is set,
   `attach` otherwise.

3. **Watermark counted blank padding.** `capture-pane` pads its output
   with blank lines down to the pane height, so `wc -l` placed the
   watermark *past* any content pi later printed → `wait` never saw
   `WORKER_DONE`. **Fix:** `pane_line_count` now counts to the index of
   the last *non-blank* line (`awk`).

4. **`send` returned before pi launched.** `tmux send-keys` is
   asynchronous; the shell needs a moment to read the line and `exec`
   pi. `send` returning immediately meant a back-to-back `send` saw
   `bash` (misjudged the worker idle, so two `pi` commands got queued
   onto one input line) and a `wait` saw `bash` (misjudged pi as already
   exited). **Fix:** `send` now blocks until `pane_current_command` is
   `pi` (or a marker has already appeared, for fast turns), with a 15s
   launch timeout.

## Known limitations / not yet tested

- **Parallel pair** (two workers on disjoint files at once) was not
  exercised end-to-end. The design supports it; the orchestrator must
  ensure file sets are disjoint (tt cannot detect overlap).
- **`BLOCKED:` path** not exercised with a real pi turn — only the grep
  logic is in place.
- **Persistent chains longer than 2 turns** not exercised.
- **Scrollback roll-past** — if a pane's content scrolls past the
  watermark before `wait` runs (very unlikely at `history-limit`
  50000), `wait`'s slice would be wrong. There is no explicit guard for
  this; consider adding one if long-running turns ever hit it.
- `tt down` reads a y/N confirmation from stdin — fine interactively,
  but a non-interactive caller must pipe `y`.
- `~/.cache/tt/` (mentioned in the original plan for a pi-capability
  probe) is **not used** — the shell model made it unnecessary.

## How to test again

```sh
TD=$(mktemp -d /tmp/tt-test-XXXX); cd "$TD"
cp ~/code/work-noeffort/projects/bassaudio/bassaudio-storefront/.pi/APPEND_SYSTEM.md \
   "$TD/.pi/" 2>/dev/null || { mkdir .pi && cp .../.pi/APPEND_SYSTEM.md .pi/; }
env -u TMUX tt up 2>/dev/null   # create detached; attach fails harmlessly off-tty
# ... exercise tt pi send/wait/status ...
tmux kill-session -t "=$(tt name)"; rm -rf "$TD" "/tmp/tt/$(tt name)"
```

Live pi steps spend Codex quota — keep tasks trivial.

## Possible next steps

- Add a guard in `wait` for the scrollback-roll-past case.
- Exercise the parallel-pair and `BLOCKED:` paths.
- Consider a `tt pi logs <cs>` verb to dump a worker's pane scrollback.
- The `dev` / `claude` windows are bare shells — a per-project optional
  config to auto-run the dev command could be added if it proves needed.

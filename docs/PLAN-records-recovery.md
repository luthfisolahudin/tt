# Plan — task records & in-place recovery

Working tracker for the two-release effort that closes the **read/recover side**
of tt's file control channel. Confirmed 2026-05-29.

**Status: complete & verified live (2026-05-29).** R1 shipped in `0.9.0`, R2 in
`0.10.0`; both verified live against the repo's own session (see STATUS
"Verified"). Reset-to-idle was scoped out at the user's call (resume recovers;
clear wipes). Kept as the consolidated design record for the effort.

## North star

> An orchestrator should dispatch, observe, collect, and recover work through one
> uniform, durable, machine-readable interface — and never lose work to timing,
> interruption, or a lost id.

tt is mature on the **write side** (task ids, per-task nonces, `tasks.jsonl`,
queue, lazy spawn) and impoverished on the **read/recover side** (one overwritten
result, busy-only wait, undifferentiated states, one-way `clear`-and-wipe at
interruption). Everything below closes that gap. A structural culprit feeds most
of it: **named workers and pool tasks diverge** — pool tasks already get durable
per-id results, named workers get one overwritten `<cs>.result`. Removing that
fork *is* the substrate.

## Architecture (one substrate, three layers)

```
Layer 3  RECOVERY      resume (tt pi resume / /tt-resume)← lifecycle is reversible  [R2]
Layer 2  COLLECTION    tt pi collect (cursor)           ← fan-out is complete       [R1]
Layer 1  OBSERVATION   state+reason · --json            ← read-projections          [R1]
──────────────────────────────────────────────────────────────────────────────────
Layer 0  THE RECORD    results/<id>.result (named==pool)← durable, addressable      [R1]
```

## Release 1 — `0.9.0` "task records & observability"  (Layers 0–2)

- **L0 substrate.** Unify named + pool onto one id-keyed store `results/<id>.result`.
  `<cs>.result` demoted to the worker's latest-pointer (keeps `worker_state`/
  liveness readers unchanged). Pool migrates off `queue-results/`. The two `wait`
  read paths collapse onto the per-id store (also fixes the "old task-id whose
  result was overwritten won't resolve" limitation).
- **L1 results.** `tt pi results [<cs>|<task-id>]` — list / re-read any past
  outcome by id (recover an id lost to compaction).
- **L1 observe.** `--json` across `results`/`status`/`wait`; `status`
  interrupted/blocked rows carry a one-line reason hint from the result text.
  (Keep the `interrupted` state word — do NOT split into `no-marker`/`errored`;
  not worth the enum churn.)
- **L2 collect.** `tt pi collect [all|<cs>]` + per-worker `<cs>.collected` cursor:
  return every result with `turn > cursor`, block on in-flight, advance cursor.
  New verb (does not redefine `wait all`'s "join busy workers" contract).

Propagation: DESIGN (result layout, collapsed wait, state-files table) · STATUS
(state layout, drop the overwrite limitation) · README table · `tt --help` ·
consumer `SKILL.md` (`results`/`collect`/`--json`) · CHANGELOG · `VERSION=0.9.0`.

## Release 2 — `0.10.0` "in-place recovery"  (Layer 3)

Resume an interrupted worker **without a context wipe**:
`idle → busy → interrupted → busy → done`.

- `tasks.jsonl` row gains `notify` so resume can re-honor `--notify`.
- `tt-worker.ts`: `resumeInterruptedTask()` — rehydrate id+nonce+notify from
  `tasks.jsonl`, `setBusy(true)`, rewrite `running`, `sendUserMessage` a
  "finish + end with WORKER_DONE nonce:<n>" continuation; existing `agent_end`
  validator closes it to `done`.
  - pi-side: `registerCommand("tt-resume", …)` (typed in the pane).
  - orchestrator: `tt pi resume <cs>` writes a `<cs>.resume` trigger the extension
    watches (like `.steer`) and funnels into the same routine.
- **Reset-to-idle scoped out** (user's call). `resume` recovers an interrupted
  task; `clear` wipes context. No `tt pi reset` / `/tt-reset`.

Propagation (done): README · `--help` · skill · DESIGN (`.resume` trigger,
`interrupted → busy → done` lifecycle, tasks.jsonl notify field) · CHANGELOG ·
`VERSION=0.10.0`.

---
name: delegating-to-pi
description: >
  Load BEFORE doing any non-trivial coding or codebase-exploration work yourself — this project has a pi worker pool that should do it instead. Whenever a task involves writing or editing files, scaffolding, codegen, refactors, renames, dead-code removal, format conversions, multi-file changes, OR searching, auditing, or tracing code across the codebase, delegate it to a pi worker via `tt pi send` rather than doing it inline or with a built-in Explore / general-purpose subagent. Default posture is delegate-first: keep a task only when it genuinely needs orchestrator judgment (goal definition, product/UX taste, final safety review).
  Also load when the user mentions pi, delegating, offloading, subagents, the tt worker pool, or the project's tmux session.
---

# Delegating to pi

pi is a worker wired to OpenAI Codex (ChatGPT Plus, flat-rate). In this
project pi runs inside a per-project tmux session managed by the `tt`
tool (`~/.local/bin/tt`). Three immortal workers are pre-spawned —
`pi-alfa`, `pi-bravo`, `pi-charlie` — plus up to two extras
(`pi-delta`, `pi-echo`) you can `tt pi add` on demand. `.pi/APPEND_SYSTEM.md`
is auto-appended from cwd, so pi already knows the Worker Mode protocol.
Recommendations below are empirical — revalidate per project.

## Delegate by default — but calibrate

Every incoming chunk of work is one of three buckets. Most
miscalibration is treating a bucket-1 task as bucket-2, or a bucket-2
task as bucket-3.

1. **Trivial / single-step** — reading one file, one grep, a known
   small edit. Do it **inline** with Read/Grep/Edit. Never spawn a
   worker or a built-in subagent for it: the send/wait/verify cycle
   costs far more than the task.
2. **Substantial & bounded** — real work with a statable SUCCESS check:
   multi-file edits, codegen, scaffolding, refactors, renames,
   dead-code removal, wide audits. **Delegate to pi.** This is the
   default for real work; doing it inline when a worker was free is the
   failure mode this skill exists to prevent.
3. **Needs orchestrator judgment** — goal definition, product/UX taste,
   architecture calls, final safety review. **Keep it** (see "Keep with
   the orchestrator").

What separates 2 from 3 is not "is this worth delegating" — assume it
is — but "does this genuinely need *me*". No concrete judgment reason?
It's bucket 2.

Why delegate at all: the payoff is **parallel fan-out** and **keeping
orchestrator context lean** — not raw single-task speed. A lone
sequential send/wait can be net-slower than doing it yourself; delegate
so you can run things in parallel and stay unburdened, not as a reflex.

Once you've decided to delegate, pick the tier:

1. Task is **safety-critical** (see triggers below)? → `tt pi send <name> --medium <prompt>`.
2. Otherwise → `tt pi send <name> <prompt>` (default `--low`).
3. Output looks wrong? → retry once at `--medium`; if still wrong, take
   the task back rather than escalating further.

## Model tiers

| Tier                     | Use when                            | Why                                                |
| ------------------------ | ----------------------------------- | -------------------------------------------------- |
| `gpt-5.5:low` (default)  | Anything not safety-critical        | ~20–25s avg; BLOCKs on impossible (mini fakes it)  |
| `gpt-5.5:medium`         | Safety-critical only                | Catches blast-radius traps low misses              |
| `gpt-5.5:high`           | Don't                               | 2–3× latency, no quality gain over medium          |
| Any mini tier            | Don't                               | Hallucinates success on impossible/ambiguous tasks |
| `gpt-5.3-codex:high`     | Don't                               | Refused a multi-file task in evals                 |

In our eval, low handled ~80% of work at or above the orchestrator's quality bar.

## Safety-critical triggers

A task is safety-critical when the "obvious wrong answer" has high
blast radius and low will pick it:

- **Dead-code / deletion** where a symbol may be imported from outside
  the main source tree (config files, server entry, build scripts,
  test harnesses). Low has removed files still imported by build glue;
  medium correctly traces the dependency.
- **TS error fixes near codegen output** — low tends to edit the
  generated file (brittle, regenerated on next codegen run); medium
  fixes the importers instead.
- **Anything touching generated/build artifacts**, or that requires
  understanding what *not* to delete.

## MUST / MUST NOT

- **MUST** use the TASK / FILES / CHANGE / [CONTEXT] / SUCCESS prompt
  format (see below).
- **MUST** parse stdout for `BLOCKED:` or `WORKER_DONE` before
  continuing.
- **MUST** verify the diff (Read or `git diff`) before committing pi's
  safety-critical output.
- **MUST NOT** escalate the model when pi returns `BLOCKED:` —
  rephrase the prompt instead.
- **MUST NOT** use vague verbs ("improve", "clean up", "fix",
  "better") in CHANGE — they produce drift.
- **MUST NOT** use a persistent worker for open-ended exploration.
  Persistent is for a short chain of bounded, SUCCESS-checked follow-ups
  (e.g. "apply the refactor" → "fix the type errors it surfaced"). The
  moment the next step needs orchestrator judgment, stop the chain and
  `tt pi clear`.

## Prompt format

```
TASK: <one imperative sentence>
FILES: <exact/path/to/file.tsx>   # single file, OR "dir/* read+write" for multi-file
CHANGE: <specific description — no "better/improve/fix/clean">
CONTEXT: <paste snippet if surgical>
SUCCESS: <one-line check>
```

## Stdout protocol

- `BLOCKED: <reason>` — task was ambiguous or impossible; rephrase and
  retry, don't escalate the model.
- `WORKER_DONE\nfiles_changed: ...\nsummary: ...\nnotes: ...` — task
  complete; verify via `git diff` or Read before continuing.

## Tasks pi handles well

- Architecture decisions — multi-point analysis with file:line
  citations and reasoned why-not for rejected options.
- Debugging unclear failures, diagnostic phase — root-causing across a
  handful of files in one shot.
- Cross-file consistency audits — bucketing many files across
  compliance dimensions.
- Multi-file mechanical refactors — e.g. cross-codebase symbol rename.
- Codegen from spec, new files, scaffolding.
- Single-file refactors — sub-component / hook extraction.
- Removal plans / dead-code analysis (text output). Medium has caught
  subtle internal uses (e.g. an exported helper used inside its own
  module) that the task author missed.

## Exploration — delegate the sweeps, keep the precise lookups

Codebase search splits three ways, same as any other work:

- **Bulk or parallel sweeps** — "audit every route handler for X",
  several disjoint searches at once, a wide map of a subsystem.
  **Delegate to pi**; fan disjoint searches across parallel workers.
- **A single precise lookup where you need trustworthy `file:line`** —
  keep it with the built-in **Explore** agent. Explore is the same
  Claude-grade model, read-only by construction, and folds its answer
  straight back into your reasoning with no verification pass. pi is a
  weaker model and has returned stale line numbers — for citations
  you'd act on, that verification tax cancels the offload.
- **One file, one grep** — inline (bucket 1 above). No subagent at all.

Truly open-ended exploration — where each step's query depends on what
the last turned up — stays with the orchestrator; it can't be written
as a single bounded task.

## Keep with the orchestrator

These are the genuine exceptions — everything else delegates. A task
stays with the orchestrator only when it needs judgment a worker
structurally cannot supply:

- **Goal definition** — "choosing what to do" is a conversation with
  the user, not a task for pi.
- **Product / brand / UX taste** — subjective calls that need your
  judgment.
- **Final safety-critical diff review** — verify pi's deletions, type
  fixes, and generated-file edits before committing.
- **Truly open-ended, step-dependent exploration** (see above) — a
  persistent worker can do a *short* bounded chain, but exploratory
  back-and-forth where the next query is unknowable up front belongs
  with the orchestrator.

## Worker pool

Each worker is a **live interactive pi REPL** in a tmux window of the
project session — the user can attach, watch, type into, and steer it by
hand at any time. The `tt` tool (`tt --help` for the full reference) is
the orchestrator's channel to them; drive workers only through `tt pi`
verbs, never by calling `pi` directly.

### Pick a worker

1. Run `tt pi status`. Pick the lowest-NATO **idle** worker
   (`alfa` → `bravo` → `charlie` → `delta` → `echo`).
2. If none is idle and you're not at the cap of 5, run `tt pi add` to
   spawn the next one. It prints the new callsign.
3. If all 5 are busy, **wait** — don't queue. Pick when one frees up.

### Choose flavor

- **Ephemeral** (default): `tt pi clear <name>` → `tt pi send` →
  `tt pi wait` → verify diff. The `clear` wipes prior context so a
  reused worker can't leak scope from earlier turns. If the worker was
  spawned via `tt pi add`, run `tt pi popidle` after to keep things
  tidy.
- **Persistent**: skip the `clear`. Reuse a worker for a series of
  bounded follow-ups (e.g. "apply the refactor" → "now fix the type
  errors it surfaced"). Each follow-up MUST restate the SUCCESS check;
  persistent workers accumulate context, so scope drift is the failure
  mode.

### Send + wait

```
TID=$(tt pi send <name> [--medium] <(cat <<'PROMPT'
TASK: ...
FILES: ...
CHANGE: ...
SUCCESS: ...
PROMPT
))
tt pi wait <name> "$TID"
```

- `--medium` for safety-critical work (see triggers below); omit for
  default `--low`.
- The task ID returned by `send` (e.g. `bravo-3`) anchors `wait` to
  the *current* turn — `wait` won't false-positive on a stale
  `WORKER_DONE` from an earlier turn in the same window.
- Process substitution beats temp files; stdin (`-`) works too.

### Parallelism

Workers may run **in parallel only if their FILES are disjoint**. The
tool can't detect overlap; the orchestrator must. Up to 3 concurrent
is comfortable; the 5-cap is a hard ceiling.

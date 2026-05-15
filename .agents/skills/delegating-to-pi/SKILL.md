---
name: delegating-to-pi
description: >
  Load BEFORE starting any coding execution that contains a mechanical subtask — creating new files/components from a spec, applying edits at known line ranges, scaffolding, codegen, mass renames, dead-code removal, format conversions.
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

## Quick-decide ladder

1. Task is **safety-critical** (see triggers below)? → `tt pi send <name> --medium <prompt>`.
2. Otherwise → `tt pi send <name> <prompt>` (default `--low`).
3. Output looks wrong? → retry once at `--medium`; if still wrong, keep
   the task with the orchestrator.

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

## Out of scope (keep with the orchestrator)

- **Open-ended multi-turn debug** where each iteration is decided by
  what the previous one revealed. A persistent worker can do a *short*
  bounded chain, but exploratory back-and-forth belongs with the
  orchestrator.
- **Product / brand / UX judgment** — needs your taste.
- **Goal definition** — "choosing what to do" is a conversation with
  the user, not a task for pi.
- **Final safety-critical diff review** — verify pi's deletions, type
  fixes, and generated-file edits before committing.

## Worker pool

Workers live in tmux windows of the project session. The `tt` tool
(`tt --help` for the full reference) is the only way the orchestrator
talks to them — never call `pi -p` directly anymore.

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

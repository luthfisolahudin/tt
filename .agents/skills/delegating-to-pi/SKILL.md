---
name: delegating-to-pi
description: >
  Load when considering delegation for substantial bounded code work, parallel fan-out, or when the user mentions pi, delegating, offloading, subagents, the tt worker pool, or the project's tmux session. Do not load for one-file reads, one grep, or known small edits. Default posture for real work is delegate-first: keep a task only when it genuinely needs orchestrator judgment (goal definition, product/UX taste, final safety review).
---

# Delegating to pi

pi is a worker wired to OpenAI Codex (ChatGPT Plus, flat-rate) — a **separate
provider and budget** from the orchestrator, so delegating offloads work off
your own context and token budget. In this project pi runs inside a per-project
tmux session managed by the `tt` tool (`~/.local/bin/tt`). The worker pool is
**lazy**: nothing is pre-spawned. A worker (callsign `alfa`…`zulu`, NATO)
materializes on first use and persists until removed; the cap is
`min(cores-2, 26)`. `pi-worker/APPEND_SYSTEM.md` is auto-appended through tt's
worker runtime, so pi already knows the Worker Mode protocol. Recommendations
below are empirical — revalidate per project.

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

Once you've decided to delegate, pick the tier by **counting the reasoning
steps** and the cost of a wrong answer:

1. **Low** (default) — routine bounded work with one obvious path; most tt
   tasks start here.
2. **Medium** — safety-critical work (see triggers), 2–4 step workflows, or
   output that will be used with little human safety net.
3. **High** — 5–8 step analytical/code work: multi-file implementation that
   must execute correctly, dependency/edge-case mapping, or multi-source
   research. Also bump to high when the embarrassment/cost of being wrong
   outweighs the wait.
4. **XHigh** — rare deep-branching work: architecture decisions, grueling
   debugging loops, complex logic/math, or novel reasoning where
   pattern-matching is not enough.
5. If output looks wrong, retry once one tier up; if it still looks wrong,
   take the task back rather than blindly escalating.

`tt` intentionally does not expose a no-thinking/none tier for workers; use
`low` as the minimum so the worker still plans enough to follow the protocol.

## Model tiers

| Tier                     | Use when                                      | Why                                                |
| ------------------------ | --------------------------------------------- | -------------------------------------------------- |
| `gpt-5.5:low` (default)  | 1-step routine tasks, predictable code edits  | Fast; in our eval low handled ~80% at quality bar  |
| `gpt-5.5:medium`         | Safety-critical or 2–4 step workflows         | Balanced; catches blast-radius traps low misses    |
| `gpt-5.5:high`           | 5–8 step analytical / multi-file work         | More dependency and edge-case checking             |
| `gpt-5.5:xhigh`          | Deep debugging, architecture, novel reasoning | Maximum self-checking; slow, use sparingly         |
| Any mini tier            | Don't                                         | Hallucinates success on impossible/ambiguous tasks |
| `gpt-5.3-codex:high`     | Don't                                         | Refused a multi-file task in evals                 |

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
- **Domain hard-gates and business rules** — regulatory/compliance gates,
  stage or state-machine transitions, permission/auth checks, financial or
  pricing calculations: logic where a plausible-looking wrong edit has real
  blast radius and low will pick the obvious-wrong answer.
- **Anything touching generated/build artifacts**, or that requires
  understanding what *not* to delete.

## Rules that prevent real failures

Each of these exists because skipping it has burned a real task — the why is
the point, not the imperative.

- Use the TASK / FILES / CHANGE / [CONTEXT] / SUCCESS / [OUTPUT] format (below) —
  free-form prompts drift.
- Wait for the `tt pi wait` status marker before continuing — otherwise you act
  on a stale or still-in-flight result.
- Summarize worker results concisely; don't paste the full WORKER_DONE block
  unless asked.
- Verify the diff (Read or `git diff`) before committing pi's safety-critical
  output — low picks the obvious-wrong answer on blast-radius tasks.
- On `BLOCKED:`, rephrase the prompt rather than escalating the model — a block
  signals ambiguity, not a capability gap.
- Avoid vague verbs ("improve", "clean up", "fix", "better") in CHANGE — they
  produce drift.
- Keep persistent workers to a short chain of bounded, SUCCESS-checked follow-ups
  (e.g. "apply the refactor" → "fix the type errors it surfaced"); the moment the
  next step needs orchestrator judgment, stop and `tt pi clear`. Never use one
  for open-ended exploration.

## Prompt format

```
TASK: <one imperative sentence>
FILES: <exact/path/to/file.tsx>   # single file, OR "dir/* read+write" for multi-file
CHANGE: <specific description — no "better/improve/fix/clean">
CONTEXT: <paste snippet if surgical>
SUCCESS: <one-line check>
OUTPUT: <optional cap, e.g. "Keep response under 20 lines" or "Top 5 findings only">
```

## Output Budget

Default to terse worker handoffs. Ask for detailed narrative only when
that detail is the deliverable.

- For implementation tasks: `OUTPUT: Terminal block only; notes only for
  risks, failed checks, dependent changes, or artifact paths.`
- For audits: set a cap such as `OUTPUT: Top 5 findings only, with file
  paths; no exhaustive narrative.`
- If a longer handoff is valuable and FILES permits `.tt/`, ask the
  worker to write `.tt/handoffs/YYYY-MM-DD/<task-id>-<slug>.md` and put
  only that path plus key risks in `notes`.

## Stdout protocol

- `BLOCKED: <reason>` — task was ambiguous or impossible; rephrase and
  retry, don't escalate the model.
- `WORKER_DONE\nfiles_changed: ...\nsummary: ...\nnotes: ...` — task
  complete; verify via `git diff` or Read before continuing. Treat it as
  machine-readable: extract the facts, then give the user a short
  synthesis rather than replaying the whole block.

## Tasks pi handles well

- Bounded architecture analysis — a stated decision question with an
  explicit output cap, file:line citations, and reasoned why-not for
  rejected options.
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

## Worker pool & tt CLI

The mechanics — picking/spawning workers, ephemeral vs persistent flavors,
send/wait/collect, steering and recovery, parallel fan-out — live in
`references/tt-cli.md`. Read it when you're ready to dispatch. The essentials
you need to *decide* with:

- Drive workers only through `tt pi` verbs — never call `pi` directly.
- `tt pi auto --rm` is the clean default for an independent task; omit the tier
  for default `--low`, and use `--medium`/`--high`/`--xhigh` only when the
  reasoning-budget guide above justifies the wait.
- Fan out across workers only when their FILES are disjoint; join the fan-out
  with `tt pi wait all` (still-busy) or `tt pi collect` (some already finished).

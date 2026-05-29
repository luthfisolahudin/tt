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
- **Domain hard-gates and business rules** — regulatory/compliance gates,
  stage or state-machine transitions, permission/auth checks, financial or
  pricing calculations: logic where a plausible-looking wrong edit has real
  blast radius and low will pick the obvious-wrong answer.
- **Anything touching generated/build artifacts**, or that requires
  understanding what *not* to delete.

## MUST / MUST NOT

- **MUST** use the TASK / FILES / CHANGE / [CONTEXT] / SUCCESS / [OUTPUT]
  format (see below).
- **MUST** check the `tt pi wait` status marker before continuing.
- **MUST** summarize worker results to the user concisely; do not paste
  the full WORKER_DONE block unless asked.
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

## Worker pool

Each worker is a **live interactive pi REPL** in a tmux window of the
project session — the user can attach, watch, type into, and steer it by
hand at any time. The `tt` tool (`tt --help` for the full reference) is
the orchestrator's channel to them; drive workers only through `tt pi`
verbs, never by calling `pi` directly.

### Pick a worker

- **Don't care which worker?** Use `tt pi auto` — it picks an idle worker,
  spawns one if none is idle (under the cap), or queues on the shared pool if
  all are busy, echoes `using pi-<cs>`, and prints the task-id to `wait` on.
  The default for an independent task.
- **Continuing a specific worker's context?** Send to it by name —
  `tt pi send <cs>`. It lazy-spawns the worker if absent and queues behind its
  current turn if busy (run-next). Use a name when the follow-up needs context
  that worker already holds.
- `tt pi status` shows the pool when you want to look.

### Choose flavor

- **One-shot (ephemeral)**: `tt pi auto --rm` spawns a fresh worker, runs the
  task, and tears it down once done — no context leak, no cleanup. The clean
  default for an independent task.
- **Fresh but persistent**: `tt pi auto --prefer-fresh` spawns a NEW worker
  (under the cap) instead of reusing an idle one — a clean pi context *without*
  `--rm`'s teardown. Reach for it on **parallel fan-out** (claim distinct workers
  eagerly rather than piling several tasks onto one worker that just freed up) and
  whenever a reused worker's leftover context could bias the new task.
- **Persistent**: `tt pi send <cs>` (or `tt pi auto` without `--rm`) leaves the
  worker alive for a short chain of bounded follow-ups (e.g. "apply the refactor"
  → "fix the type errors it surfaced"). Each follow-up MUST restate the SUCCESS
  check; persistent workers accumulate context, so scope drift is the failure
  mode. `tt pi clear <cs>` wipes context mid-chain; `tt pi rm <cs>` removes the
  worker when the chain is done.

### Send + wait

```
# Named (continuation) — lazy-spawns, queues behind a busy turn:
tt pi send <cs> [--medium] [--notify] - <<'PROMPT'
TASK: ...
FILES: ...
CHANGE: ...
SUCCESS: ...
PROMPT
tt pi wait <cs>            # task-id optional → waits on the latest dispatch

# Or let tt pick the worker; capture the id it returns:
TID=$(tt pi auto [--medium] [--rm] - <<<'TASK: ...')
tt pi wait "$TID"          # works for a callsign id (alfa-3) or pool id (pool-3)
```

- `--medium` for safety-critical work (see triggers); omit for default `--low`.
- `wait` accepts a callsign (latest task), a bare task-id (`alfa-3`, **any** id —
  even an older one resolves), a pool id (`pool-3`), or `all` (join every busy
  worker in one report). It anchors on the task-id, so it won't false-positive on
  a stale `WORKER_DONE`. Add `--json` for a parsed envelope
  (`{id,status,summary,files_changed,notes,reason}`) instead of the raw block.
- `--notify` makes dispatch fire-and-forget: the worker pings this session when
  it finishes, so you can move on instead of blocking on `wait`.
- **Lost the task-id** (e.g. after a compaction)? `tt pi results` lists every
  recorded outcome (newest first); `tt pi results <id>` re-reads one. Nothing
  depends on having kept the id from dispatch.
- Inline prompts use `-` (stdin) with a heredoc/here-string, **not** process
  substitution.
- **Steer a running worker**: `tt pi steer <cs> - <<<'...'` injects a correction
  into its *current* turn (run-now), vs `send` which queues it for next.
- **Recover an interrupted worker**: if a worker shows `interrupted` (a turn
  ended without a clean `WORKER_DONE`/`BLOCKED` — e.g. someone hit Esc in its
  pane), use `tt pi resume <cs>` to re-drive that task to completion **with its
  context intact** (`interrupted → busy → done`), then `tt pi wait <cs>`. Reach
  for `tt pi clear <cs>` only when you actually want a fresh, context-wiped REPL.

### Parallelism

Workers run **in parallel only if their FILES are disjoint** — the tool can't
detect overlap; the orchestrator must. Fan out with several `tt pi send`s (or
`tt pi auto`s), then **join them in one call with `tt pi wait all`** rather than
one `wait` per worker. The cap is `min(cores-2, 26)`; past it, `auto` queues on
the shared pool for the next free worker to claim.

`wait all` joins only workers busy *at the instant it runs* — if some tasks may
have finished already (fast tasks, or a `--notify` batch), use **`tt pi collect`**
instead: it returns every result you have not collected yet (blocking on
in-flight ones) via a per-worker cursor, so a fan-out is joined completely
without you tracking each id. Add `--json` to either for parsed envelopes.

# Prompting the `minimax` tier (premium)

`--tier minimax` runs `opencode-go/minimax-m3` at **high** effort — the
premium tier, picked for harder or longer-horizon work where the
model's higher base capability earns its way past its own lower
thinking effort (high vs xhigh) and past `deepseek` for the same
work.

For the tier overview see [prompting-and-tiers.md](prompting-and-tiers.md).
For the general prompt contract (`TASK` / `TARGET STATE` / `FILES / SCOPE` / `CHANGE` / `DO NOT` / `SUCCESS` / `VERIFY`)
see [prompting-and-tiers.md#prompt-contract](prompting-and-tiers.md#prompt-contract);
this file only covers what `minimax` specifically needs.

## Tier-specific

- **Interleaved Thinking is on.** The model reasons between every
  tool call, reflecting on output before the next step. Do not ask
  it to "think step by step" or "show your reasoning" — redundant
  instruction degrades output.
- **Role + criteria, not just role.** "Senior backend reviewer focused
  on correctness, reliability, and maintainability" produces
  different output than "experienced engineer". Pin the *criteria*
  the model should optimize, not just the experience level.
- **Output contract.** Section names, column names, bullet limits,
  length caps. A downstream workflow (a `BLOCKED` rewrite, a review,
  a handoff file) will parse the output — spell out the shape so
  the model doesn't have to guess.
- **Stop rules for multi-iteration work.** Even though `tt` workers
  don't expose tools directly, when a prompt implies
  verify-then-fix or plan-then-execute loops, set explicit stopping
  conditions. The provider warns against overeagerness: "use tools
  only when they materially improve the answer".
- **Long context — task after source.** When pasting large source
  material, place the task *after* the source and ask the model to
  quote or summarize the relevant parts before answering. Grounding
  in quotes cuts noise and makes verification easier.
- **Vague SUCCESS hurts more here.** The model's higher capability
  means it produces a *more confident* wrong answer if the prompt
  is ambiguous. Falsifiability matters even more than on `deepseek`.

## Model-selection criteria

The tier choice is **per task, not per favorite**.

### Default to `deepseek` when

- Bounded refactor with explicit scope and a single goal.
- Audit or sweep with a clear success criterion.
- Codegen from a clear spec.
- Focused debug across a handful of files with a known reproduction.
- Parallel fan-out where each task is small and bounded.

### Pick `minimax` when

- The framing is part of the answer: an ambiguous spec, a
  "redesign X" task, or work where the right scope itself is
  the question.
- Multi-file architecture change where the dependency graph
  matters.
- Long-horizon agentic work: implement, verify, fix, re-verify
  across many files; the success criterion is a multi-step check,
  not a one-shot grep.
- Judgment-heavy work: choose between competing valid designs,
  evaluate tradeoffs, or write handoff content for a human
  reviewer.
- `deepseek` has been seen to miss the right framing on a
  well-written prompt. That's a signal to retry on `minimax`,
  not to keep iterating on the same tier.

### Do not switch tiers to fix a bad prompt

If a worker returns `BLOCKED:` or drift, fix the prompt first.
Switching from `deepseek` to `minimax` to "give the model more rope"
on an under-specified prompt will just produce a more confident
wrong answer. Sharpen the prompt; then, if the prompt is sharp
and the work is genuinely beyond `deepseek`'s class, retry on
`minimax`.

> **Tier change on a running worker requires a respawn.** The REPL
> is launched with `--model $provider:$effort` baked into the launch
> command. `tt pi send` / `auto` **refuses** a `--tier NAME` that
> would change an existing worker's tier — the error points at
> `tt pi clear <cs>`, which respawns the REPL on a fresh session-dir
> (context is lost, like a normal `clear`). This catches the case
> where a silent wrong-model dispatch would otherwise happen. To
> switch a worker's tier: `tt pi clear <cs>` first, then send with
> the new `--tier NAME`.

## Sample prompts

### Architecture migration (premium for design)

```text
TASK: Migrate the auth flow from library A to library B without
breaking the existing session, OAuth, and CSRF semantics.

CONTEXT / SOURCES: Library B uses a different session model (signed cookies vs
  server-side stores) and a different CSRF story. The migration must
  preserve: existing session cookies across rolling deploys, OAuth
  callback behavior, and the CSRF protection on POSTs. Cost of getting
  this wrong = real user logouts and possible CSRF regression. Sources:
- `src/auth/**` — current implementation
- `src/middleware/session.ts` — session lifecycle
- `src/middleware/csrf.ts` — CSRF check
- `docs/auth.md` — current documented behavior
- `tests/auth/**` — current test coverage (must all pass)

TARGET STATE: Library B serves the auth flow without changing public APIs,
  invalidating active sessions, or weakening OAuth/CSRF behavior.
FILES / SCOPE: src/auth/**, src/middleware/session.ts,
  src/middleware/csrf.ts, docs/auth.md, tests/auth/**
CHANGE: Implement the migration with a one-release compatibility flag and
  update the existing tests and documentation.
DO NOT:
- Do not change the public API of the auth module.
- Do not break rolling deploys: any active session at deploy time
  must still resolve.
- Do not delete the old library yet; leave it behind a flag for
  one release.
SUCCESS: Existing auth tests pass and a pre-migration session resolves after
  restart on the migrated implementation.
VERIFY: Run the existing auth test suite, then a manual session
  round-trip check (`login` → restart server → `me`).
OUTPUT:
1. Migration plan — 5 bullets maximum, ordered by risk.
2. Files to change — table with File, Change, Risk.
3. Verification steps — shell commands the worker can run.
4. Open questions — only items that need an orchestrator decision.
```

### Ambiguous-spec synthesis (premium for framing)

```text
TASK: Recommend one of three candidate designs for the new event-sourcing
  layer. Read the linked discussions, identify the deciding
  tradeoff for each, and recommend one.

CONTEXT / SOURCES: The deciding factor is operator cognitive load over the
  next 18 months, not peak throughput. Cost of getting this wrong
  = six months of an unmaintainable system. Sources:
- `docs/design-a.md` (2026-04-12)
- `docs/design-b.md` (2026-04-15)
- `docs/design-c.md` (2026-04-19)

TARGET STATE: One recommendation grounded in operator cognitive load, with
  the deciding tradeoff and source conflicts made explicit.
FILES / SCOPE: docs/design-a.md, docs/design-b.md, docs/design-c.md
CHANGE: Compare the three designs and recommend one; prefer the newest dated
  source when sources conflict.
DO NOT: Do not optimize for peak throughput or implement any design.
SUCCESS: Every design has a cited deciding tradeoff and the recommendation
  follows the stated operator-cognitive-load criterion.
VERIFY: Re-read all three sources and check every claim in the recommendation
  against a cited source.
OUTPUT:
- Recommendation — 1 sentence, with the deciding tradeoff named.
- For each design — 3 bullets: deciding tradeoff, what it costs,
  what it buys.
- Open questions — items that need an orchestrator decision.
```

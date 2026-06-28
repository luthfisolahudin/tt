# Prompting the `minimax` tier (premium)

`--tier minimax` runs `opencode-go/minimax-m3` (MiniMax-M3) at **high**
effort. This is the **premium tier** — picked for harder or
longer-horizon work where the model's higher base capability earns its
way past `deepseek`'s lower thinking budget. It is **not** a faster or
cheaper path to the same answer; it is a different quality band.

For the tier overview and when to pick `deepseek` instead, see
[prompting-and-tiers.md](prompting-and-tiers.md). For tier mechanics on
the CLI, see [tt-cli.md](tt-cli.md).

## Why this tier exists alongside `deepseek`

From production model-routing practice: a single model wastes money
when routed everywhere, and a single cheap model fails on hard tasks.
The two tiers in `tt` are exactly this split — `deepseek` for
high-volume, bounded work; `minimax` for harder work where the model's
higher base capability earns its way. The classic framing: **premium
for design, cheap for execution**. `minimax` is the design-tier.

Concretely: `deepseek` at xhigh is good at structured work with clear
scope (bounded refactors, audits, codegen from a spec, mechanical
sweeps). `minimax` at high is better at work where the *framing* is
itself part of the answer — ambiguous specs, architecture decisions,
multi-file judgment calls, anything where `deepseek` has been seen to
miss the right framing on a well-written prompt.

## Model characteristics (MiniMax-M3)

- **MoE 428B / 23B active** — frontier-class base capability; this is
  what you pay for on the premium tier.
- **1M-token context with MiniMax Sparse Attention (MSA)** — 9×
  prefill and 15× decode speedup vs M2 at 1M, with per-token compute
  at ~1/20. Long-context work is genuinely usable here, not just
  theoretically supported.
- **Native multimodality** — text, image, video. For `tt` workers this
  is text-only, but it means the model is comfortable with
  mixed-modality context if you paste it.
- **Interleaved Thinking** — the model reasons *between* every tool
  call, reflecting on tool output before deciding the next step. This
  is what makes M3 strong on long-horizon agentic benchmarks
  (SWE, BrowseCamp, xBench).
- **Three reasoning modes** (enabled / adaptive / disabled) — the
  `tt` deployment pins this tier to `enabled` at `high` effort; the
  worker extension calls `pi.setThinkingLevel("high")` at task claim.
  Do not ask the model to "think step by step" in the prompt —
  interleaved thinking is already on, and redundant instruction
  degrades output.
- **Recommended inference params** (from the provider): `temperature=1.0`,
  `top_p=0.95`, `top_k=40`. These are the model's house defaults; the
  worker REPL does not override them, so you can rely on this style.

## Prompt structure

Follow the contract in [prompting-and-tiers.md](prompting-and-tiers.md#prompt-contract).
On `minimax`, the prompt should be *more* structured than on `deepseek`
— the model uses your structure to decide where to spend its
interleaved thinking.

### MiniMax's golden rule

> Show your prompt to a colleague who has no context on the task. If
> they would be confused, the model will be too.

This is the provider's own framing and it carries over directly.
`minimax` will infer missing context — but you don't want it to have
to. Spell out: the role it should play, the constraints that matter
and *why* they matter, the output contract, and the stop rules.

### Sections to use (and the order)

MiniMax's docs recommend flat, labeled sections — bold headers or
labels with a trailing colon. The exact labels are not sacred, but the
*kinds* of information are:

- **Task** — what to do, one imperative sentence.
- **Context** — what the model needs to know that isn't in the files.
  Include *why* a constraint matters when the constraint is non-obvious
  (the docs are explicit: context for formatting/safety/accessibility
  constraints is especially high-leverage).
- **Source** — for long context work, place this *before* the task.
  Index and delimit long sources ("`launch-plan` — 2026-04-12" then the
  body) so the model can quote or summarize specific parts.
- **Constraints** — what to do and what NOT to do. Be explicit about
  scope boundaries.
- **Output format** — section names, table columns, bullet limits,
  length caps. Avoid "be detailed" or "be concise" without a number.
- **Role** — define expertise and decision criteria, not just "you
  are a senior engineer". A role works best when it pins the
  *criteria* the model should optimize (correctness, reliability,
  maintainability, scope discipline, …).

### What to emphasize

- **Role + criteria** — `minimax` uses the role to weight its
  interleaved thinking. "Senior backend reviewer focused on
  correctness, reliability, and maintainability" produces different
  output than "experienced engineer". Pick criteria, not just
  experience level.
- **Output contract** — section names, column names, bullet limits.
  A downstream workflow (a `BLOCKED` rewrite, a review, a handoff
  file) will parse the output; spell out the shape so the model
  doesn't have to guess.
- **Stop rules for tool use / agentic flow** — even though `tt`
  workers don't expose tool definitions directly, when you write a
  prompt that involves multiple iterations (verify-then-fix loops,
  plan-then-execute), set explicit stopping conditions. MiniMax's
  docs warn against overeagerness: "use tools only when they
  materially improve the answer".
- **Long context** — when pasting large source material, place the
  task *after* the source and ask the model to quote or summarize
  the relevant parts before answering. Grounding in quotes cuts
  noise and makes verification easier.
- **Examples for ambiguous tasks** — 3-5 diverse few-shot examples
  beat abstract style instructions for classification, structured
  extraction, or anything with edge cases.

### What to avoid

- **Do not ask the model to "show reasoning" or "think step by step"**
  — interleaved thinking is already on. Redundant instructions
  degrade output.
- **Do not paste the whole repo** when you can point at files. MSA
  makes 1M context cheap, but the model's effective attention is
  still best when the relevant slice is named.
- **Do not bundle multiple independent goals.** `minimax` will
  happily start a multi-pronged plan; you almost always want one
  dispatch per goal so the result is verifiable per goal.
- **Do not use vague SUCCESS criteria.** The model's higher capability
  means it will produce a *more confident* wrong answer if the
  prompt is ambiguous. Falsifiability matters even more here than
  on `deepseek`.
- **Do not switch to `minimax` to compensate for a bad prompt.** If
  the work is bounded and the prompt is clear, `deepseek` is fine
  and cheaper. The premium tier is for harder work, not for
  rescuing under-specified prompts.

## Model-selection guidance: when to actually pick `minimax`

This is the core of the tier choice. The rule is **per task, not per
favorite**.

### Default to `deepseek` when

- Bounded refactor with explicit scope and a single goal.
- Audit or sweep with a clear success criterion (e.g. "every
  reference to X in src/ is updated").
- Codegen from a clear spec.
- Focused debug across a handful of files with a known reproduction.
- Mechanical work where every field in the prompt can be made
  falsifiable.
- Parallel fan-out where each task is small and bounded.

### Pick `minimax` when

- The framing is part of the answer: an ambiguous spec, a
  "redesign X" task, or work where the right scope itself is
  the question.
- Multi-file architecture change where the dependency graph
  matters: "migrate Y from library A to library B" where every
  file Y is used in has to be considered.
- Long-horizon agentic work: implement, verify, fix, re-verify
  across many files; the worker's success criterion is a
  multi-step check, not a one-shot grep.
- Judgment-heavy work: choose between competing valid designs,
  evaluate tradeoffs, or write handoff content for a human
  reviewer. The model's higher base capability is what you're
  paying for here.
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

## Sample prompts

### Architecture migration (premium for design)

```text
Task: Migrate the auth flow from library A to library B without
breaking the existing session, OAuth, and CSRF semantics.

Context: Library B uses a different session model (signed cookies vs
  server-side stores) and a different CSRF story. The migration must
  preserve: existing session cookies across rolling deploys, OAuth
  callback behavior, and the CSRF protection on POSTs. Cost of getting
  this wrong = real user logouts and possible CSRF regression.

Role: senior backend engineer focused on correctness and
  backward-compatible rollout.

Source:
- `src/auth/**` — current implementation
- `src/middleware/session.ts` — session lifecycle
- `src/middleware/csrf.ts` — CSRF check
- `docs/auth.md` — current documented behavior
- `tests/auth/**` — current test coverage (must all pass)

Constraints:
- Do not change the public API of the auth module.
- Do not break rolling deploys: any active session at deploy time
  must still resolve.
- Do not delete the old library yet; leave it behind a flag for
  one release.

Output format:
1. Migration plan — 5 bullets maximum, ordered by risk.
2. Files to change — table with File, Change, Risk.
3. Verification steps — shell commands the worker can run.
4. Open questions — only items that need an orchestrator decision.

VERIFY: Run the existing auth test suite, then a manual session
  round-trip check (`login` → restart server → `me`).
```

### Judgment-heavy code review (premium for review)

```text
Task: Review the diff below and identify the highest-impact issues.

Role: senior reviewer focused on correctness, security, and
  maintainability. Do not rewrite files. Only flag what is
  in the diff.

Context: This is a candidate change to the file-upload pipeline.
  Cost of getting the review wrong = a missed security or
  correctness bug ships to production.

Diff:
[diff]

Output format:
1. Summary — 3 bullets maximum
2. Blocking issues — table with File, Risk, Recommendation
3. Non-blocking suggestions — 5 bullets maximum
4. Open questions — items that need an orchestrator decision

Do not include chain-of-thought or unrelated exploration in the
final answer.
```

### Ambiguous-spec synthesis (premium for framing)

```text
Task: We have three candidate designs for the new event-sourcing
  layer. Read the linked discussions, identify the deciding
  tradeoff for each, and recommend one.

Context: The deciding factor is operator cognitive load over the
  next 18 months, not peak throughput. Cost of getting this wrong
  = six months of an unmaintainable system.

Source:
- `docs/design-a.md` (2026-04-12)
- `docs/design-b.md` (2026-04-15)
- `docs/design-c.md` (2026-04-19)

If sources conflict, prefer the newest dated source and call out
the conflict in the Open questions section.

Output format:
- Recommendation — 1 sentence, with the deciding tradeoff named.
- For each design — 3 bullets: deciding tradeoff, what it costs,
  what it buys.
- Open questions — items that need an orchestrator decision.

Keep the reasoning visible but concise.
```

## When NOT to pick `minimax`

- High-volume, structured work where `deepseek` is sufficient. You
  are paying a real cost in latency and tokens for the premium
  tier; don't pay it for work that doesn't need it.
- Tight latency budgets. `minimax` at high effort is slower per
  token than `deepseek` at xhigh on the same input. If the
  orchestrator is waiting on the worker interactively, prefer
  `deepseek` unless the work is genuinely beyond it.
- Parallel fan-out of small bounded tasks. Use `deepseek` (or
  `auto --prefer-fresh` with `deepseek`) for parallelism; the
  premium tier is not the right axis for throughput.
- Under-specified prompts. The model's higher capability will
  produce a more confident wrong answer. Fix the prompt first.

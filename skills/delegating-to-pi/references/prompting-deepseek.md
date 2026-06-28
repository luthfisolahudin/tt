# Prompting the `deepseek` tier (default)

`--tier deepseek` runs `opencode-go/deepseek-v4-flash` at **xhigh** effort.
This is the cost-efficient default for high-volume, structured work —
bounded refactors, audits, codegen from a clear spec, dead-code sweeps,
focused debugs across a handful of files, and any task with explicit scope
and a falsifiable success check.

For the tier overview and when to pick `minimax` instead, see
[prompting-and-tiers.md](prompting-and-tiers.md). For tier mechanics on
the CLI, see [tt-cli.md](tt-cli.md).

## Model characteristics

- **MoE 284B / 13B active** — fast inference, low per-token cost; well
  suited to many parallel workers.
- **1M-token context** — can hold a whole repo, but the model's effective
  attention still degrades past the first ~200K tokens. For large
  contexts, point it at a slice (`FILES: src/foo/*`) instead of dumping
  the whole tree.
- **xhigh thinking effort is baked in** — the model already plans and
  self-checks before answering at this effort. Do not ask it to
  "think step by step" in the prompt; the runtime is already doing
  that. Adding "think step by step" or "show your reasoning" is
  redundant and can degrade output quality.
- **Strong on structured prompts** — responds well to labeled fields
  (TASK / FILES / CHANGE / SUCCESS / VERIFY), XML/CO-STAR-style framing,
  and explicit constraints. Vague prose produces vague output.

## Prompt structure

Follow the contract in [prompting-and-tiers.md](prompting-and-tiers.md#prompt-contract).
On `deepseek`, lean especially hard on structure: the model's xhigh
effort gives you planning for free, so spend your prompt budget on
**scope precision** (what to touch, what NOT to touch) and
**verifiability** (a self-check the worker can run).

### What to emphasize

- **TASK** — one imperative sentence naming the single goal. If you
  have multiple independent goals, split into separate dispatches.
- **FILES** — every file the worker may touch. Use `dir/*` only when
  the worker can safely touch ANY file in that directory. List callers,
  importers, and test files explicitly when they need updating.
- **CHANGE** — concrete, narrow. Avoid weasel words ("improve", "fix",
  "clean up", "better", "optimize"). Prefer "rename X to Y", "add Z
  parameter to function A", "remove B and update all callers". The
  model takes every field literally.
- **SUCCESS** (required) — a falsifiable check the worker can run
  against their own diff. Examples: "no remaining references to the
  old function name", "build passes with no new warnings", "every
  renamed export has exactly one call-site updated". Not "code is
  cleaner" (not falsifiable).
- **VERIFY** (recommended) — a shell command or prompted review step.
  `pnpm tsc --noEmit`, `cargo check`, `grep -r OLD_NAME src/`, or "re-read
  your diff and check for any stale imports". On `deepseek`, a VERIFY
  step is high-leverage: the model is good at executing mechanical
  checks when told exactly what to run.

### What to avoid

- **Do not ask the model to "show reasoning" or "think step by step"**
  — xhigh effort already does this. The extra prompt noise hurts.
- **Do not paste huge code dumps into CONTEXT** when you can point at
  a file with FILES. The 1M context is there for genuine long-context
  work, not for skipping the FILES field.
- **Do not bundle multiple independent goals** in one prompt.
  Split into separate dispatches.
- **Do not use vague SUCCESS criteria.** If the worker cannot
  falsify it against their own diff, the prompt is the problem.
- **Do not assume the model knows the project's conventions.** Spell
  out the framework, the test runner, the lint command, the file
  layout. xhigh effort is for reasoning, not for inferring your
  stack from context.

## Sample prompts

### Bounded refactor with explicit scope

```text
TASK: Rename the `parseArgs` helper to `parseCliArgs` across the codebase.
FILES: src/cli/parse.ts, src/commands/*.ts, tests/cli/*.test.ts
CHANGE: Rename `parseArgs` to `parseCliArgs` in src/cli/parse.ts and update
  every importer to use the new name. Do not change the function signature
  or its behavior. Do not rename unrelated `parse*` helpers.
CONTEXT: This is a 3-day-old rename; no public docs reference the old name.
SUCCESS: `grep -r "parseArgs" src/ tests/` returns zero matches.
VERIFY: Run `pnpm typecheck` and `pnpm test src/cli/`. Both must pass.
OUTPUT: Terminal block only; notes only for any renames you skipped and why.
```

### Dead-code audit

```text
TASK: Identify dead code in src/legacy/ that has no remaining importers
  in src/, tests/, or scripts/.
FILES: src/legacy/**, src/**, tests/**, scripts/**
CHANGE: For each function or export in src/legacy/ that has zero
  importers outside src/legacy/, list it with its file:line and a
  one-line reason. Do not delete anything. Do not flag symbols that
  are still imported by tests, scripts, or any package.json entry point.
SUCCESS: Every entry in your report includes file:line and at least one
  grep command that returned zero matches outside src/legacy/.
VERIFY: Re-run the greps yourself before reporting. If a flag turns out
  to have a remaining importer, remove it from the report.
OUTPUT: Top findings only (most-impactful first), with file paths.
  Cap at 20 entries.
```

### Focused debug

```text
TASK: Fix the race condition in src/sync/queue.ts that causes the
  "duplicate delivery" error under concurrent enqueue.
FILES: src/sync/queue.ts, src/sync/queue.test.ts
CHANGE: Add a per-key mutex around the dequeue+ack path so two
  concurrent dequeue() calls for the same key cannot both return
  the same item. Do not change the public API. Do not touch the
  producer side.
CONTEXT: Reproduction is in the test file; run `pnpm test
  src/sync/queue.test.ts` to see the failing case. The bug was
  introduced in commit abc123.
SUCCESS: `pnpm test src/sync/queue.test.ts` passes, including the
  new concurrent-dequeue test case.
VERIFY: Run the full test suite (`pnpm test`) to confirm no regression
  in other sync tests.
OUTPUT: Terminal block only; notes only for any test you added or any
  race you found that was NOT the target race.
```

## When NOT to pick `deepseek`

- Multi-file architecture changes where the right framing is itself
  the question. `minimax`'s higher base capability is better at this.
- Work that needs sustained judgment across many ambiguous decisions
  (e.g. "redesign the auth flow"). The model's xhigh effort helps
  with planning, but the base model still needs to make the right
  judgment calls.
- Anything where you've seen `deepseek` miss the right framing on
  the same well-written prompt. That is a signal to retry on
  `minimax`, not to keep iterating on the same tier.

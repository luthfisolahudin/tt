# Prompting, tiers, and result handling

Use this after `SKILL.md` says to delegate. Keep prompts narrow: pi performs best when the job has explicit scope, a concrete change, and a falsifiable success check.

## Tier

All workers run at **xhigh** — `opencode-go/deepseek-v4-flash` at maximum
reasoning effort. Tier flags (`--low`/`--medium`/`--high`/`--xhigh`) are
rejected; there is no tier to choose.

Treat these as safety-critical contexts that warrant extra care in the prompt:

- Dead-code/deletion where build scripts, config, tests, or entrypoints may still import the symbol.
- Type fixes near generated/codegen output; prefer fixing importers over edited generated files.
- Domain hard-gates: auth/permission, regulatory/compliance, workflow state, finance/pricing, or other business-critical logic.
- Anything touching generated/build artifacts, or requiring knowing what *not* to delete.

## Prompt contract

Use this shape; vague prompts drift. Every prompt MUST be unambiguous and
MUST give the worker a concrete way to falsify their own work.

```text
TASK: <one imperative sentence>
FILES: <exact/path.ts>          # or "dir/* read+write" for multi-file scope
CHANGE: <specific change; avoid "improve/fix/clean up/better">
CONTEXT: <optional surgical snippet or constraint>
SUCCESS: <one-line check that the worker can verify themselves>
VERIFY: <recommended self-check the worker runs to prove correctness>
OUTPUT: <optional cap, e.g. "Terminal block only; notes only for risks/checks">
```

### Field guide

**TASK** — One imperative sentence naming the single goal. If you have multiple
independent goals, split into separate dispatches. If you find yourself writing
"and also", it is two tasks.

**FILES** — Every file the worker may touch. Use `dir/*` only when the worker
can safely touch ANY file in that directory. If the change affects callers or
importers, list them too — the worker stops at the listed files.

**CHANGE** — What to do, be concrete enough that a wrong implementation is
detectable. Avoid weasel words like "improve", "fix", "clean up", "better",
"optimize". Prefer "rename X to Y", "add Z parameter to function A", "remove B
and update all callers".

  ✗ Bad: "Fix the error handling in the payment module"
  ✓ Good: "Add a try-catch around the Stripe API call in pay.ts, log the error,
    and return a 500 response with {error: message}"

**CONTEXT** — Only include what the worker needs to understand the task that is
not already in the files. Prefer surgical snippets over long explanations.

**SUCCESS** (required) — A falsifiable check the worker can run against their
**own diff** before reporting done. If the worker cannot self-verify without
outside help, it is too vague.

  ✓ "No remaining references to the old function name"
  ✓ "Build passes with no new warnings"
  ✓ "Every renamed export has exactly one call-site updated"
  ✗ "Code is cleaner" (not falsifiable)
  ✗ "User should have a better experience" (not checkable by the worker)

**VERIFY** (recommended — include it) — A self-check the worker runs to confirm
correctness before reporting `WORKER_DONE`. Can be:

- A **shell command**: `pnpm tsc --noEmit`, `cargo check`, `grep -r OLD_NAME src/`
- A **prompted review step**: "Re-read your diff and check for any stale
    imports" or "Search all files for remaining references to the old name"

  Include VERIFY whenever there is something the worker can mechanically or
  analytically check. If you skip it, you accept that the worker will not
  self-correct.

**OUTPUT** (optional) — Caps what the worker returns. Use when the default
terminal-block verbosity is more than you need.

### Before you send — checklist

Read your prompt one more time and check each:

1. **TASK: is it one thing?** If you have "and also", split.
2. **FILES: did you miss any?** Callers, importers, test files — the worker
   stops at the listed files.
3. **CHANGE: can it be interpreted narrowly?** Assume the most literal reading.
   If you mean "every location" and you wrote "the location", you will get one.
4. **SUCCESS: can the worker check this against their own diff?** If they need
   a human reviewer, it is too vague.
5. **VERIFY: is there a shell command or review step you can add?** Type-check,
   grep for stale refs, re-read the diff — include it.
6. **Would you send a follow-up to fix this?** If yes, fix the prompt instead.

### Output caps

- Implementation: `OUTPUT: Terminal block only; notes only for risks, failed checks, dependent changes, or artifact paths.`
- Audit: `OUTPUT: Top 5 findings only, with file paths; no exhaustive narrative.`
- Long handoff: if `FILES` allows `.tt/`, ask the worker to write a handoff file and return only its path plus key risks.

## Good fits

- Capped architecture/design analysis with file citations and why-not tradeoffs; the orchestrator still makes the decision.
- Focused diagnostic debugging across a handful of files.
- Cross-file consistency audits and mechanical refactors.
- Codegen/scaffolding from a clear spec.
- Removal plans / dead-code analysis before deletion.

Search/exploration split:

- Bulk or parallel sweeps: delegate.
- One precise lookup needing trustworthy `file:line`: do it inline; verify pi citations if used.
- One file / one grep: inline.
- Open-ended, step-dependent exploration: keep with orchestrator.

## Result protocol

- `BLOCKED: <reason>` — ambiguous/impossible; rewrite the task.
- `WORKER_DONE\nfiles_changed: ...\nsummary: ...\nnotes: ...` — completed; extract the facts, verify as needed, then report concisely.

Do not accept a worker result just because it says `WORKER_DONE`. For code changes, inspect `git diff` or targeted reads; for risky behavior, run checks or reproduce the relevant scenario when practical.

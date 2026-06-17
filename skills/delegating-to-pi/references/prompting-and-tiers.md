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
VERIFY: <recommended command the worker runs to prove correctness>
OUTPUT: <optional cap, e.g. "Terminal block only; notes only for risks/checks">
```

### Clarity rules

- TASK must name the **single goal** in one sentence. If the task has multiple
  independent goals, split into separate dispatches.
- FILES must list every file the worker may touch. Use `dir/*` only when the
  worker can safely touch any file in that directory.
- CHANGE must say **what** to do, not how — but be concrete enough that a wrong
  implementation is detectable. Avoid weasel words like "improve", "fix",
  "clean up", "better", "optimize". Prefer "rename X to Y", "add Z parameter
  to function A", "remove B and update all callers".
- SUCCESS must be a **falsifiable check** the worker can run against their own
  diff — e.g. "No remaining references to the old name", "build passes",
  "the exported type matches the schema". If the worker cannot self-verify,
  the SUCCESS is too vague.
- VERIFY (recommended) is a shell command the worker executes to confirm the
  change is correct — e.g. `pnpm tsc --noEmit`, `cargo check`, `grep -r
  OLD_NAME src/`. Include a VERIFY whenever there is a mechanical check the
  worker can run (linter, type-check, grep for stale references, etc.).

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

# Prompting, tiers, and result handling

Use this after `SKILL.md` says to delegate. Keep prompts narrow: pi performs best when the job has explicit scope, a concrete change, and a falsifiable success check.

## Tier guide

| Tier | Use when |
| --- | --- |
| `--low` (default) | Routine bounded work with one obvious path. |
| `--medium` | Safety-critical edits, 2–4 step workflows, or output with little human safety net. |
| `--high` | 5–8 step analytical / multi-file work, dependency mapping, costly wrong answers. |
| `--xhigh` | Rare: architecture, deep debugging, complex logic/math, novel reasoning. |

If output looks wrong, retry once one tier up; if still wrong, take the task back.

Treat these as at least **medium**:

- Dead-code/deletion where build scripts, config, tests, or entrypoints may still import the symbol.
- Type fixes near generated/codegen output; prefer fixing importers over edited generated files.
- Domain hard-gates: auth/permission, regulatory/compliance, workflow state, finance/pricing, or other business-critical logic.
- Anything touching generated/build artifacts, or requiring knowing what *not* to delete.

## Prompt contract

Use this shape; vague prompts drift.

```text
TASK: <one imperative sentence>
FILES: <exact/path.ts>          # or "dir/* read+write" for multi-file scope
CHANGE: <specific change; avoid "improve/fix/clean up/better">
CONTEXT: <optional surgical snippet or constraint>
SUCCESS: <one-line check>
OUTPUT: <optional cap, e.g. "Terminal block only; notes only for risks/checks">
```

Good output caps:

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

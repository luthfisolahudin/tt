# Prompting the default worker

The default worker runs `cosmoshub/qwen-3.7-max` at **max** effort for all
delegated work. Normal dispatches omit `--tier`.

For the tier overview see [prompting-and-tiers.md](prompting-and-tiers.md).
For the general prompt contract (`TASK` / `TARGET STATE` / `FILES / SCOPE` / `CHANGE` / `DO NOT` / `SUCCESS` / `VERIFY`)
see [prompting-and-tiers.md#prompt-contract](prompting-and-tiers.md#prompt-contract);
this file only covers what the current default specifically needs.

## Model-specific

- **Max effort already plans and self-checks.** Do not ask the model
  to "think step by step" or "show your reasoning" — the runtime is
  doing that. Redundant instruction degrades output. Spend your
  prompt budget on **scope precision** and **verifiability** instead.
- **VERIFY is high-leverage here.** The model is good at executing
  mechanical checks when told exactly what to run. A concrete
  `VERIFY:` line (`pnpm tsc --noEmit`, `grep -r OLD_NAME src/`,
  re-read your diff for stale imports) is the single highest-leverage
  thing you can add to a worker prompt.
- **Strong on structure.** Labeled fields, explicit scope, and
  concrete CHANGE clauses work better than prose. The prompt contract
  in the overview doc is the floor; lean into it.

## Sample prompts

### Bounded refactor

```text
TASK: Rename the `parseArgs` helper to `parseCliArgs` across the codebase.
TARGET STATE: The helper and every importer use `parseCliArgs`; runtime behavior is unchanged.
FILES / SCOPE: src/cli/parse.ts, src/commands/*.ts, tests/cli/*.test.ts
CHANGE: Rename `parseArgs` to `parseCliArgs` in src/cli/parse.ts and update
  every importer to use the new name.
DO NOT: Do not change the function signature or behavior. Do not rename unrelated `parse*` helpers.
SUCCESS: `grep -r "parseArgs" src/ tests/` returns zero matches.
VERIFY: Run `pnpm typecheck` and `pnpm test src/cli/`. Both must pass.
OUTPUT: Terminal block only; notes only for any renames you skipped and why.
```

### Dead-code audit

```text
TASK: Identify dead code in src/legacy/ that has no remaining importers
  in src/, tests/, or scripts/.
TARGET STATE: A deletion candidate report only; no code is changed.
FILES / SCOPE: src/legacy/**, src/**, tests/**, scripts/**
CHANGE: For each function or export in src/legacy/ that has zero
  importers outside src/legacy/, list it with its file:line and a
  one-line reason.
DO NOT: Do not delete anything. Do not flag symbols that are still imported by tests,
  scripts, or any package.json entry point.
SUCCESS: Every entry in your report includes file:line and at least one
  grep command that returned zero matches outside src/legacy/.
VERIFY: Re-run the greps yourself before reporting. If a flag turns out
  to have a remaining importer, remove it from the report.
OUTPUT: Top findings only (most-impactful first), with file paths.
  Cap at 20 entries.
```

# Prompting, tiers, and result handling

Use this after `SKILL.md` says to delegate. Keep prompts narrow: pi performs best when the job has explicit scope, a concrete change, and a falsifiable success check.

## Tier

A **tier** is a named preset that bundles (model, thinking effort). The
effort is fixed per tier and cannot be set independently — the legacy
`--low`/`--medium`/`--high`/`--xhigh`/`--max` flags are rejected with a pointer
to `--tier`. Stable tiers:

- **`default`** — `9router/cx/gpt-5.6-luna` at `max` effort. It handles
  all delegated worker tasks and accepts text/image input. See
  [prompting-default.md](prompting-default.md).

Pick a tier per dispatch with `--tier NAME` on `tt pi send` / `tt pi
auto`. Omit `--tier` to keep the worker's current tier (a fresh worker
starts on `default`).

There is no escalation tier. Do not compensate for a bad prompt by inventing a
model override: sharpen the prompt, retry fresh when needed, and keep product or
architecture judgment with the orchestrator. The
[model decision](../../../docs/MODEL_DECISION.md) owns the periodic choice of
default model.

### Safety-critical prompting

Independent of tier, treat these as safety-critical contexts that
warrant extra care in the prompt:

- Dead-code/deletion where build scripts, config, tests, or entrypoints may still import the symbol.
- Type fixes near generated/codegen output; prefer fixing importers over edited generated files.
- Domain hard-gates: auth/permission, regulatory/compliance, workflow state, finance/pricing, or other business-critical logic.
- Anything touching generated/build artifacts, or requiring knowing what *not* to delete.

## Prompt contract

Use this shape; vague prompts drift. Every prompt MUST be unambiguous and
MUST give the worker a concrete way to falsify their own work.

```text
TASK: <one imperative sentence>
CONTEXT / SOURCES: <why this exists; relevant source docs/snippets>
TARGET STATE: <expected product/technical end-state>
FILES / SCOPE: <exact/path.ts or bounded area; use SCOPE/SOURCES if files are unknown>
CHANGE: <specific change; avoid "improve/fix/clean up/better">
DO NOT: <explicit exclusions and boundaries>
SUCCESS: <enumerated acceptance requirements: R1, R2, ...>
VERIFY: <required for code changes: one check per requirement, scoped diff inspection, and relevant tests>
OUTPUT: <optional report/artifact format cap>
```

### Field guide

**TASK** — One imperative sentence naming the single goal. If you have multiple
independent goals, split into separate dispatches. If you find yourself writing
"and also", it is two tasks.

**CONTEXT / SOURCES** — Why this task exists, the user/product rule behind it,
and the source docs/snippets the worker should trust. Keep it surgical; do not paste
long background when a file path or quoted rule is enough.

**TARGET STATE** — The expected end-state from the product/technical point of view.
This is where user-story acceptance criteria belong. Do not hide the desired result
inside `OUTPUT`; `OUTPUT` only shapes the worker's report.

**FILES / SCOPE** — Every file the worker may touch, or a bounded area/source set
when the exact files are not known yet. Use `dir/*` only when the worker can safely
touch ANY file in that directory. If the change affects callers or importers, list
them too — the worker stops at the listed files. If discovery is still needed, make
that the task and do not authorize implementation yet.

**CHANGE** — What to do, concrete enough that a wrong implementation is detectable.
Avoid weasel words like "improve", "fix", "clean up", "better", "optimize".
Prefer "rename X to Y", "add Z parameter to function A", "remove B and update all
callers".

  ✗ Bad: "Fix the error handling in the payment module"
  ✓ Good: "Add a try-catch around the Stripe API call in pay.ts, log the error,
    and return a 500 response with {error: message}"

**DO NOT** — Explicit boundaries: files/behaviour not to touch, scope that is deferred,
and product choices the worker must not reinterpret.

**SUCCESS** (required) — A falsifiable check the worker can run against their
**own work** before reporting done (and its own diff for code changes). If the
worker cannot self-verify without outside help, it is too vague.

When a requirement has independent dimensions or cases, enumerate each one in
`SUCCESS` or `VERIFY`. Do not expect a generic phrase such as "conflicting
inputs are rejected" to prove that source, target, and amount conflicts were
all exercised; a worker may correctly implement all three while testing only a
representative subset.

  ✓ "No remaining references to the old function name"
  ✓ "Build passes with no new warnings"
  ✓ "Every renamed export has exactly one call-site updated"
  ✗ "Code is cleaner" (not falsifiable)
  ✗ "User should have a better experience" (not checkable by the worker)

**VERIFY** — Required for code changes and recommended for read-only tasks. It
is the self-check the worker runs to confirm correctness before reporting
`WORKER_DONE`. It MUST:

- map each `SUCCESS` item to a targeted test or review check;
- inspect the scoped diff, or re-read every changed file when no diff tool is available;
- run the relevant behavioral and static checks;
- treat a requested command that cannot be invoked as a failed check.

It can use:

- A **shell command**: `pnpm tsc --noEmit`, `cargo check`, `grep -r OLD_NAME src/`
- A **prompted review step**: "Re-read your diff and check for any stale
    imports" or "Search all files for remaining references to the old name"

  Every requested check must pass. If one fails, cannot run, or is skipped, fix
  and rerun it when the task permits; otherwise report the check and cause in
  `notes`. Do not silently replace a requested check with a weaker substitute.
  `notes: none` is valid only when every requested check passed and every
  requested fact was established. A later green check does not erase an
  earlier failure.

**OUTPUT** (optional) — Caps what the worker returns. Use when the default
terminal-block verbosity is more than you need.

### Before you send — checklist

Read your prompt one more time and check each:

1. **TASK: is it one thing?** If you have "and also", split.
2. **TARGET STATE: is the expected result explicit?** If not, the worker will infer taste.
3. **FILES / SCOPE: did you miss any?** Callers, importers, test files — the worker
   stops at the listed files.
4. **CHANGE: can it be interpreted narrowly?** Assume the most literal reading.
   If you mean "every location" and you wrote "the location", you will get one.
5. **DO NOT: did you block likely drift?** Name deferred scope and things not to redesign.
6. **SUCCESS: can the worker check this against its own work?** If they need
   a human reviewer, it is too vague. Does every distinct required case map to
   an explicit assertion or check?
7. **VERIFY: is there a shell command or review step you can add?** Type-check,
   grep for stale refs, re-read the diff — include it.
8. **Would you send a follow-up to fix this?** If yes, fix the prompt instead.

### Follow-ups

Reuse the full prompt contract for corrective work. Reference the prior task as
evidence, not as unstated context:

```text
CONTEXT / SOURCES: Review of alfa-3 found R1 source-conflict coverage missing;
R2 target and R3 amount already pass.
```

Authorize only the unresolved delta and give it its own `SUCCESS` and `VERIFY`.
Persistent REPL context helps continuity, but it does not replace a
self-contained prompt.

### Output caps

- Implementation: `OUTPUT: Terminal block only; notes only for risks, failed checks, dependent changes, or artifact paths.`
- Audit: `OUTPUT: Top 5 findings only, with file paths; no exhaustive narrative.`
- Long handoff: if `FILES / SCOPE` allows `.tt/`, ask the worker to write a handoff file and return only its path plus key risks.

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

Do not accept a worker result just because it says `WORKER_DONE`. For code
changes, acceptance requires both implementation coverage and test/assertion
coverage for every `SUCCESS` item. Inspect the actual diff or targeted files,
then run the relevant checks independently. If a required command failed, was
omitted, or the terminal report conflicts with observed execution, inspect
`tt pi logs <cs>` before acceptance. Never infer success from `notes: none`.

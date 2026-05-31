---
name: delegating-to-pi
description: >
  Load when deciding whether to delegate to pi workers: substantial bounded code work, parallel fan-out, or explicit mentions of pi, tt workers, subagents, offloading, or the project tmux session. Skip for one-file reads, one grep, and known tiny edits. Default: delegate bounded execution; keep goal/product/architecture judgment and final review.
---

# Delegating to pi

pi workers are live Codex REPLs in the project tmux session, managed by `tt`.
They use a separate provider/budget, are lazy-spawned on first use, and already
know the Worker Mode protocol. Drive them only through `tt pi` verbs. Operational
mechanics live in `references/tt-cli.md`; this file is for deciding what to send,
which tier to use, and how to prompt.

## Decide: inline, delegate, or keep

- **Inline:** one file read, one grep/search, or a known tiny edit. Delegation
  overhead costs more than the work.
- **Delegate:** substantial, bounded execution with a statable `SUCCESS` check:
  multi-file edits, scaffolding, refactors, audits, dead-code analysis, focused
  debugging, or parallel sweeps.
- **Keep with orchestrator:** goal definition, product/UX taste, architecture
  choice, final safety review, or open-ended exploration where each next query
  depends on the last result.

Delegate for **parallelism and lean orchestrator context**, not because a single
sequential worker turn is faster.

## Pick the tier

| Tier | Use when |
| --- | --- |
| `--low` (default) | Routine bounded work with one obvious path. |
| `--medium` | Safety-critical edits, 2–4 step workflows, or output with little human safety net. |
| `--high` | 5–8 step analytical / multi-file work, dependency mapping, costly wrong answers. |
| `--xhigh` | Rare: architecture, deep debugging, complex logic/math, novel reasoning. |

If output looks wrong, retry once one tier up; if still wrong, take the task back.

Treat these as at least **medium**:

- Dead-code/deletion where build scripts, config, tests, or entrypoints may still
  import the symbol.
- Type fixes near generated/codegen output; prefer fixing importers over edited
  generated files.
- Domain hard-gates: auth/permission, regulatory/compliance, workflow state,
  finance/pricing, or other business-critical logic.
- Anything touching generated/build artifacts, or requiring knowing what *not* to
  delete.

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

- Implementation: `OUTPUT: Terminal block only; notes only for risks, failed
  checks, dependent changes, or artifact paths.`
- Audit: `OUTPUT: Top 5 findings only, with file paths; no exhaustive narrative.`
- Long handoff: if `FILES` allows `.tt/`, ask the worker to write a handoff file
  and return only its path plus key risks.

## Operating rules

- Default independent task: `tt pi auto --rm [--medium|--high|--xhigh] -`.
- For fan-out, ensure `FILES` are disjoint; join with `tt pi wait all` or
  `tt pi collect` if some may already be done.
- Wait for the `tt pi wait` result marker before acting on output.
- Summarize worker results; don't paste the full `WORKER_DONE` block unless asked.
- Verify safety-critical diffs with `git diff` or targeted reads before accepting.
- On `BLOCKED:`, clarify/rephrase; don't blindly escalate the tier.
- Persistent workers are only for short bounded follow-up chains. When judgment is
  needed or scope drifts, stop and `tt pi clear <cs>`.

## What pi handles well

- Capped architecture/design analysis with file citations and why-not tradeoffs.
- Focused diagnostic debugging across a handful of files.
- Cross-file consistency audits and mechanical refactors.
- Codegen/scaffolding from a clear spec.
- Removal plans / dead-code analysis before deletion.

Search/exploration split:

- Bulk or parallel sweeps: delegate.
- One precise lookup needing trustworthy `file:line`: use the read-only Explore
  agent or do it inline; verify pi citations if used.
- One file / one grep: inline.
- Open-ended, step-dependent exploration: keep with orchestrator.

## Result protocol

- `BLOCKED: <reason>` — ambiguous/impossible; rewrite the task.
- `WORKER_DONE\nfiles_changed: ...\nsummary: ...\nnotes: ...` — completed;
  extract the facts, verify as needed, then report concisely.

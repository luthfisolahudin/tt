---
name: delegating-to-pi
description: >
  Load when deciding whether to delegate to pi workers: substantial bounded code work, parallel fan-out, dead-code/refactor/audit sweeps, focused debugging, or explicit mentions of pi, tt workers, subagents, offloading, worker pool, or the project tmux session. NOT for one-file reads, one precise grep, known tiny edits, open-ended product/architecture judgment, or final safety review. Default: delegate bounded execution; keep goal/product/architecture judgment and verification.
---

# Delegating to pi

Choose whether and how to offload bounded execution to visible `tt pi` workers while the orchestrator keeps judgment and review.

## When to use

Use when work can be bounded with files, a concrete change, and a success check: multi-file edits, scaffolding, refactors, audits, dead-code analysis, focused debugging, or parallel sweeps. Work inline for one file read, one precise grep, or a known tiny edit. Keep product/UX taste, architecture trade-offs, open-ended step-dependent exploration, and final safety review with the orchestrator.

## Rules

- Delegate for **parallelism and lean orchestrator context**, not because one sequential worker turn is faster.
- A delegated task MUST have bounded `FILES`, a specific `CHANGE`, and concrete `SUCCESS`; use the prompt contract in [prompting-and-tiers.md](references/prompting-and-tiers.md).
- Tier choice MUST be deliberate and follow [prompting-and-tiers.md](references/prompting-and-tiers.md); do not default risky work without checking the guide.
- Fan-out MUST follow the disjoint-scope rules in [tt-cli.md](references/tt-cli.md); if overlap is possible, serialize, narrow the scopes, or keep the work.
- Worker output MUST be summarized and verified before being accepted; never paste raw `WORKER_DONE` blocks unless asked.
- On `BLOCKED:` or drift, clarify/rephrase the task; do not blindly escalate the tier.
- Persistent workers SHOULD be reserved for short context-bearing follow-up chains; stop and clear when judgment is needed or scope drifts.

## Workflow

1. Decide: inline, delegate, or keep. If delegating, choose a tier using [prompting-and-tiers.md](references/prompting-and-tiers.md).
2. Write a bounded prompt with `TASK / FILES / CHANGE / SUCCESS` and any output cap.
3. Dispatch through `tt pi` only; choose the exact `auto`/`send`/`wait`/`collect` command from [tt-cli.md](references/tt-cli.md).
4. Wait for or collect results, then verify with `git diff`, targeted reads, or checks appropriate to risk.
5. Report the extracted result, files touched, verification, and any risks or blocked follow-ups.

## Out of scope

- Letting a worker decide goals, product direction, architecture trade-offs, or final acceptance.
- Delegating unbounded exploration before it has a success check.
- Changing `tt` worker mechanics; this skill only decides and operates delegation.

## Reference index

- [prompting-and-tiers.md](references/prompting-and-tiers.md) — tier guide, prompt contract, output caps, result protocol, and good-fit tasks.
- [tt-cli.md](references/tt-cli.md) — exact `tt pi` commands for dispatch, waiting, collection, status, logs, and recovery.

## Done means

Inline/keep/delegate was chosen deliberately. Delegated work had bounded scope, an appropriate tier, and a concrete success check; results were collected, summarized, and verified before being accepted. Safety-critical or drifting work stayed under orchestrator review.

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
- A **tier** is a named preset that bundles (model, thinking effort); pick it with `--tier NAME` on `tt pi send` / `tt pi auto`. Two tiers ship: `deepseek` (default, `opencode-go/deepseek-v4-flash` at xhigh) and `minimax` (`opencode-go/minimax-m3` at high, the premium tier). The legacy `--low`/`--medium`/`--high`/`--xhigh` flags are rejected — effort is fixed per tier. See [prompting-and-tiers.md](references/prompting-and-tiers.md) for the tier overview, [prompting-deepseek.md](references/prompting-deepseek.md) and [prompting-minimax.md](references/prompting-minimax.md) for per-tier prompting guidance.
- **Your prompt is the single source of truth.** Put everything you need in one prompt. Do not send an incomplete prompt expecting to fix it with follow-ups — the worker acts on what you wrote, not what you meant. If you find yourself sending a second message to correct the first, the original prompt was the problem.
- **Assume the worker takes every field literally.** A narrow interpretation is the default. If CHANGE says "update the function" and you also want to update its callers and its type signature, list those explicitly. If you want a broad search, say "every file that references X" not just the obvious file.
- **Review your prompt for ambiguity before sending.** Read each field and imagine how someone with no outside context could misinterpret it. If TASK, CHANGE, or SUCCESS could mean more than one thing, sharpen it. A `BLOCKED` or wrong output means the prompt was not clear enough.
- **Give the worker a way to verify their own work.** Every prompt MUST include a concrete SUCCESS check the worker can run against their own diff before reporting done, and **should include a VERIFY** step (a shell command or prompted review) — see [prompting-and-tiers.md](references/prompting-and-tiers.md#prompt-contract). If the worker cannot self-verify, the check is too vague.
- **Be precise about mandatory vs recommended.** "Optional" means skip it. "Recommended" means include it unless you have a concrete reason not to. If you mean "must", say MUST.
- Fan-out MUST follow the disjoint-scope rules in [tt-cli.md](references/tt-cli.md); if overlap is possible, serialize, narrow the scopes, or keep the work.
- Worker output MUST be summarized and verified before being accepted; never paste raw `WORKER_DONE` blocks unless asked.
- On `BLOCKED:` or drift, clarify/rephrase the task; if the work is beyond the chosen tier's strengths, retry on the other tier via `--tier NAME` (do not edit the prompt to compensate for a wrong tier).
- Persistent workers SHOULD be reserved for short context-bearing follow-up chains; stop and clear when judgment is needed or scope drifts.

## Workflow

1. Decide: inline, delegate, or keep. If delegating, pick a tier — `deepseek` (default) for high-volume structured work, `minimax` for harder/longer-horizon/judgment-heavy work. See the per-tier guides in `references/`.
2. Write a bounded prompt with all fields — see the prompt contract in [prompting-and-tiers.md](references/prompting-and-tiers.md#prompt-contract). Include `SUCCESS` (required) and `VERIFY` (recommended).
3. **Review the prompt before sending.** Read TASK/CHANGE and imagine the narrowest literal interpretation. Check that SUCCESS is something the worker can falsify against their own output. If you'd need a follow-up to fix what comes back, fix the prompt now instead.
4. Dispatch through `tt pi` only; choose the exact `auto`/`send`/`wait`/`collect` command from [tt-cli.md](references/tt-cli.md).
5. Wait for or collect results, then verify with `git diff`, targeted reads, or checks appropriate to risk.
6. Report the extracted result, files touched, verification, and any risks or blocked follow-ups.

## Out of scope

- Letting a worker decide goals, product direction, architecture trade-offs, or final acceptance.
- Delegating unbounded exploration before it has a success check.
- Changing `tt` worker mechanics; this skill only decides and operates delegation.

## Reference index

- [prompting-and-tiers.md](references/prompting-and-tiers.md) — tier overview, prompt contract, output caps, result protocol, and good-fit tasks.
- [prompting-deepseek.md](references/prompting-deepseek.md) — per-tier prompting guide for the `deepseek` tier (default).
- [prompting-minimax.md](references/prompting-minimax.md) — per-tier prompting guide for the `minimax` tier (premium).
- [tt-cli.md](references/tt-cli.md) — exact `tt pi` commands for dispatch, waiting, collection, status, logs, and recovery.

## Done means

Inline/keep/delegate was chosen deliberately. Delegated work had bounded scope, a concrete success check, and a VERIFY step. The original prompt was reviewed for ambiguity before sending. Results were collected, summarized, and verified before being accepted. Safety-critical or drifting work stayed under orchestrator review.

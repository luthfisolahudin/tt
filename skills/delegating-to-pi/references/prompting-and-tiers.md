# Prompting, tiers, and result handling

Use this after `SKILL.md` says to delegate. Keep prompts narrow: pi performs best when the job has explicit scope, a concrete change, and a falsifiable success check.

## Tier

A **tier** is a named preset that bundles (model, thinking effort). The
effort is fixed per tier and cannot be set independently — the legacy
`--low`/`--medium`/`--high`/`--xhigh`/`--max` flags are rejected with a pointer
to `--tier`. Stable tiers:

- **`deepseek`** (default) — `opencode-go/deepseek-v4-flash` at `xhigh`
  effort. Cost-efficient default for high-volume, structured work. See
  [prompting-deepseek.md](prompting-deepseek.md) for how to prompt it.
- **`minimax`** — `opencode-go/minimax-m3` at `high` effort. Premium
  tier for harder or longer-horizon work; positioned above `deepseek`
  even at lower effort, because the model's higher base capability
  earns its way. See [prompting-minimax.md](prompting-minimax.md) for
  how to prompt it.

Opt-in CosmosHub benchmark tiers have no routing recommendation yet:

- `cosmos-deepseek-flash` — `cosmoshub/deepseek-v4-flash` at `max`
- `cosmos-deepseek-pro` — `cosmoshub/deepseek-v4-pro` at `max`
- `cosmos-glm` — `cosmoshub/glm-5.2` at `max`
- `cosmos-kimi` — `cosmoshub/kimi-k2.7-code` at `high` (always-thinking)
- `cosmos-mimo` — `cosmoshub/mimo-v2.5` at `xhigh`
- `cosmos-mimo-pro` — `cosmoshub/mimo-v2.5-pro` at `xhigh`
- `cosmos-qwen` — `cosmoshub/qwen-3.7-max` at `xhigh`

Use the general prompt contract below for benchmark tiers. Keep default/escalation
decisions on `deepseek`/`minimax` until benchmark evidence is accepted.

Pick a tier per dispatch with `--tier NAME` on `tt pi send` / `tt pi
auto`. Omit `--tier` to keep the worker's current tier (a fresh worker
starts on `deepseek`).

### When to switch

- Default to `deepseek` for high-volume, structured work — bounded
  refactors, audits, codegen from a clear spec, dead-code sweeps, focused
  debugs across a handful of files.
- Switch to `minimax` for harder or longer-horizon work where the
  model's higher base capability earns its way past its own lower
  thinking effort (high vs xhigh) — multi-file architecture changes,
  ambiguous specs, work that needs more judgment, or anything where
  you've seen `deepseek` miss the right framing.
- Do not switch tiers to compensate for a bad prompt. If a worker
  returns `BLOCKED:` or drift, fix the prompt first; only escalate the
  tier if the same well-written prompt genuinely underperforms.

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
SUCCESS: <one-line pass/fail check the worker can verify themselves>
VERIFY: <recommended self-check the worker runs to prove correctness>
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
2. **TARGET STATE: is the expected result explicit?** If not, the worker will infer taste.
3. **FILES / SCOPE: did you miss any?** Callers, importers, test files — the worker
   stops at the listed files.
4. **CHANGE: can it be interpreted narrowly?** Assume the most literal reading.
   If you mean "every location" and you wrote "the location", you will get one.
5. **DO NOT: did you block likely drift?** Name deferred scope and things not to redesign.
6. **SUCCESS: can the worker check this against its own work?** If they need
   a human reviewer, it is too vague.
7. **VERIFY: is there a shell command or review step you can add?** Type-check,
   grep for stale refs, re-read the diff — include it.
8. **Would you send a follow-up to fix this?** If yes, fix the prompt instead.

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

Do not accept a worker result just because it says `WORKER_DONE`. For code changes, inspect `git diff` or targeted reads; for risky behavior, run checks or reproduce the relevant scenario when practical.

# Worker model decision

This document owns the periodic choice of the model behind `tt`'s single
`default` tier. The runtime source of truth remains `PI_TIER_REGISTRY` in `tt`;
this is the evidence and decision record, not a second configuration surface.

## Current decision

- Tier: `default`
- Provider/model: `cosmoshub/qwen-3.7-max`
- API: Anthropic Messages
- Thinking effort: `max`
- Decided: 2026-07-14

Qwen 3.7 Max is the best current balance for delegated coding work. It completed
all four benchmark tasks, gave the most accurate shared-pool analysis, remained
competitive on planning and editing, and used almost the same measured tokens
as DeepSeek V4 Pro while finishing faster. The small unit-price differences are
not large enough to outweigh correctness, tool reliability, or retries.

The worker remains text-only. Use Gemini directly through Pi or OpenCode when a
task genuinely requires image input; do not add a permanent vision tier without
a recurring worker use case and benchmark evidence.

## Decision rule

Candidates must first pass these hard gates:

- Complete every benchmark without a blank final answer or unrecovered API/tool
  error.
- Use Pi tools correctly through the configured provider API.
- Preserve explicit scope and output constraints.
- Pass the bounded edit's syntax and behavioral checks.

Score candidates that pass the gates in this order:

1. Correctness and source grounding: 40%.
2. Task completion and tool reliability: 25%.
3. Instruction and scope adherence: 15%.
4. End-to-end latency: 10%.
5. Effective cost, including token volume and retries: 10%.

Price per million tokens is not the cost by itself. A cheaper model that emits
more tokens, takes longer, or needs one retry can cost more operationally than a
model with a slightly higher unit price.

## Benchmark: 2026-07-14

Environment:

- Pi `0.80.6`, private worker config, CosmosHub Anthropic Messages endpoint.
- Highest configured thinking level for each reasoning model.
- Identical prompts and tool allowlists per candidate.
- Token totals are Pi-reported estimates across assistant turns. Active time is
  the sum of model/tool-turn timestamps, not wall-clock setup time.
- An initial shared-concurrency run caused provider 429s for two candidates;
  affected tasks were rerun separately and only successful reruns are counted.

Tasks:

1. Audit the tier source of truth and current code/docs consistency.
2. Analyze mixed-tier shared-pool behavior without inventing recorded metadata.
3. Plan eight fresh audits under a cap of four without losing ephemeral results.
4. Fix numeric task ordering in a bounded Bash fixture and pass its tests.

Kimi K2.7 Code, Gemini 3.5 Flash, and base MiMo V2.5 were not shortlisted: each
had a same-price family candidate with a stronger quality position. Reconsider
them when provider behavior or evidence changes, rather than assuming the model
name proves relative quality.

Results:

- **Qwen 3.7 Max**: 4/4 usable answers; 279,880 tokens; 66.8 s active;
  approximately Rp84 at Rp300/1M. Best pool analysis, good audit, workable edit,
  and fastest balanced result. Selected.
- **DeepSeek V4 Pro**: 4/4; 281,902 tokens; 84.4 s; approximately Rp85 at
  Rp300/1M. Best cap-aware plan, but invented a tier-cost mismatch in the pool
  analysis and used word-splitting in the Bash fix.
- **DeepSeek V4 Flash**: 4/4; 415,398 tokens; 251.5 s; approximately Rp125 at
  Rp300/1M. Produced the strongest minimal Bash fix, but missed documented
  inconsistencies and proposed sequential waits that can lose sibling results.
- **Gemini 3.1 Pro**: 3/4 usable final answers; 459,542 tokens; 133.0 s;
  approximately Rp115 at Rp250/1M. Strongest consistency audit and robust Bash
  edit, but one planning run ended with no final text. Retain for image tasks,
  not as the worker default.
- **MiMo V2.5 Pro**: 4/4 after a rate-limit retry; 141,166 tokens; 310.3 s;
  approximately Rp28 at Rp200/1M. Best detailed ephemeral-result analysis and
  lowest measured cost, but too slow for the default and used `ls` parsing in
  the edit.

Price snapshot supplied on 2026-07-14:

- Rp300/1M: Qwen 3.7 Max, DeepSeek V4 Flash, DeepSeek V4 Pro, Kimi K2.7 Code.
- Rp250/1M: Gemini 3.1 Pro, Gemini 3.5 Flash.
- Rp200/1M: MiMo V2.5, MiMo V2.5 Pro.

## Re-evaluate when

- The provider, API protocol, or default model changes.
- Unit price changes materially, or observed token volume changes effective
  cost by at least 25% relative to the selected model.
- The selected model has repeated blank turns, tool failures, malformed worker
  footers, or rate-limit behavior not shared by other candidates.
- A new model plausibly improves coding-agent quality, not just chat quality.
- A recurring image-input worker use case appears.
- Pi changes model/tool protocol behavior enough to invalidate prior results.

## Re-evaluation procedure

1. Keep the benchmark tasks and verification criteria identical; update them
   only when the real worker workload changes.
2. Run candidates sequentially or below the provider rate limit.
3. Record model/version, provider/API, effort, prices, completion count, output
   quality, Pi-reported tokens, retries, and active time.
4. Reject candidates that fail a hard gate before comparing price.
5. Change only the selected row in `PI_TIER_REGISTRY`, then update this record,
   current-state docs, and model-specific prompting guidance.
6. Spawn a fresh worker and verify its process command, Pi footer, and one
   tracked send/wait turn. Existing workers must be cleared to adopt a new model.

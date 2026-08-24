# Worker model decision

This document owns the periodic choice of the model behind `tt`'s single
`default` tier. The runtime source of truth remains `TierRegistry` in
`internal/worker/tier.go`; this is the evidence and decision record, not a
second configuration surface.

## Current decision

- Tier: `default`
- Provider/model: `9router/cx/gpt-5.6-terra`
- API: OpenAI Chat Completions (`<root>/v1/chat/completions`; adapter base `<root>/v1`)
- Client thinking effort: `max`
- Resolver wire effort: `max` (live Pi-route verification pending)
- Input: text and image (advertised; live Pi-route verification pending)
- Decided: 2026-08-14

The operator-directed switch was validated by the paired Luna/Terra benchmark
below. Terra matched Luna on the four capability tasks, completed the measured
run with about 28% fewer reported input/output tokens and 37% less recorded turn
time, and produced the smaller passing Bash edit. Both models needed one retry
on the hardest planning task, so this result supports Terra on efficiency
without claiming a reliability advantage. 9Router discovers models from active
provider connections, but the default remains pinned so a changing catalog
cannot silently change worker behavior.

### Current setup: 2026-08-24

Pi now follows OpenCode through the OpenAI-compatible route: catalog
`<root>/v1/models`, adapter base `<root>/v1`, and inference
`/v1/chat/completions`. The adapter sends `reasoning_effort` and replays
`reasoning_content`; provider/model IDs and the Terra `max` default remain
unchanged. Its startup fallback contains DeepSeek V4 Flash plus Terra and is
replaced by exact active membership after a successful catalog refresh. The
legacy `anthropic-compatible-providers.ts` loader name remains only to avoid a
duplicate pi auto-load migration. OpenCode already uses this route and is not
managed by tt.

The dated benchmark below records the then-current transport and remains valid
only for its measured model comparison. Live equivalence of tool, image, and
effort behavior through this new Pi route is pending until tested.

The canonical measurement record lives in
`../../9router-integrations/docs/EFFORT_MATRIX.md`. The five CodeBuddy CN models
were normalized by 9Router to `THINK:xhigh`, while the three active OpenAI Codex
models accepted explicit `max` requests. The shared resolver therefore transmits
`max` for the pinned Terra model.

Pi's model contract supports text and image input only. Audio, video, and native
document inputs remain outside the worker path even when an upstream model
advertises them.

## Benchmark: 2026-08-14

Environment:

- Fresh tt workers through the same 9Router OpenAI Codex connection, Anthropic
  Messages compatibility path, and `max` effort.
- Identical clones at commit `7301d27`, identical prompts and tool access, and
  sequential paired runs to avoid provider concurrency effects.
- Reported token totals sum Pi's input and output footer counters; cache-read
  counters are excluded. Turn time is the result channel's `duration_s`.
- Terra's first audit worker was ephemeral and its footer became unavailable;
  one additional fresh audit supplied its token estimate. The original audit's
  168-second duration remains in the run total.

Tasks:

1. Audit the tier source of truth and current code/docs consistency.
2. Analyze hypothetical mixed-tier shared-pool routing from dispatch to claim.
3. Plan eight fresh audits under a cap of four without losing durable results.
4. Fix numeric task ordering in a bounded Bash fixture and pass its tests.

Results:

- **Luna:** 4/4 tasks after one retry; approximately 1.49M reported input/output
  tokens and 1,699 seconds across five attempts. Its audit had the broadest
  first-pass drift finding and its Bash edit handled task bodies defensively,
  but the first planning attempt ended on an incomplete upstream stream and the
  successful retry took 718 seconds.
- **Terra:** 4/4 tasks after one retry; approximately 1.08M reported input/output
  tokens and 1,068 seconds across five scored attempts. It gave the most complete
  mixed-tier trace and the smallest passing Bash edit. Its first planning answer
  contained a correct plan but attached `WORKER_DONE` to preceding text, so the
  result channel correctly classified it non-terminal and required a retry.
- Both models found the stale Bash-era registry names, correctly identified the
  unqualified shared-pool misrouting risk in a hypothetical multi-tier registry,
  rejected a single eight-task pipeline at a cap of four, and passed the Bash
  syntax and behavioral tests.
- The sample is too small to establish a reliability difference: each model had
  one failed first attempt on the same hard task. Terra is retained because it
  showed no material capability regression while reducing measured token volume
  by about 28% and turn time by about 37%. The 9Router catalog reports zero
  per-token cost for both entries, with the same 272K context and 128K output
  limits.

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

- The provider, API protocol or route behavior, or default model changes; a
  Pi-route migration requires a live equivalence check before relying on prior
  tool, image, or effort evidence.
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
5. Change only the selected row in `TierRegistry`, then update this record,
   current-state docs, and model-specific prompting guidance.
6. Spawn a fresh worker and verify its process command, Pi footer, and one
   tracked send/wait turn. For a Pi-route migration, also verify tool, image,
   and effort behavior before carrying forward earlier evidence. Existing
   workers must be cleared or respawned to adopt a new runtime or model.

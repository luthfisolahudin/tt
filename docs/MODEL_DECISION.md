# Worker model decision

This document owns the periodic choice of the models behind `tt`'s tier
registry. The runtime source of truth remains `TierRegistry` in
`internal/worker/tier.go`; this is the evidence and decision record, not a
second configuration surface.

## Current decision

- **Default / Orbit Tier (`default`, `orbit`, `flash`, `gemini`):**
  - Provider/model: `9router/ag/gemini-3.8-flash-high`
  - API: OpenAI Chat Completions (`<root>/v1/chat/completions`; adapter base `<root>/v1`)
  - Context window: 1,048,576 tokens (~1M tokens)
  - Max completion tokens: 65,536 declared; effective 64,000 (the Antigravity executor clamps `maxOutputTokens` to 64,000)
  - Client thinking effort: `max`
  - Applied effort: pi transmits `reasoning_effort=max`; 9Router translates it to Gemini `thinkingLevel:high` (Gemini's highest tier), observable as `THINK:high` in the router request log
  - Input: text and image (live-verified)
  - Role: Default worker and wide-horizon tier — 1M context, high speed/quota efficiency, repo-wide sweeps, and multi-module refactoring.
  - Decided: 2026-09-03

- **Strato Tier (`strato`, alias `deepseek`):**
  - Provider/model: `commandcode/deepseek/deepseek-v4.1-flash`
  - API: Command Code Provider API, OpenAI Chat Completions (`https://api.commandcode.ai/provider/v1/chat/completions`)
  - Context window: 1,000,000 tokens (~1M tokens)
  - Max completion tokens: 64,000
  - Client thinking effort: `high`
  - Input: text and image (live-verified through the Command Code Provider API)
  - Auth: `COMMANDCODE_API_KEY`, copied into the worker tmux environment through `TT_PI_ENV_VARS`
  - Role: Secondary tier for bounded code tasks, precision edits, and surgical refactoring.
  - Decided: 2026-09-13

The operator-directed update adopts an **Altitude Theme** (`orbit` as default / wide-horizon workhorse and `strato` as targeted/bounded workhorse). `gemini-3.8-flash-high` serves as the default worker with an expansive 1M-token context window, excellent agentic coding benchmarks, and favorable quota capacity. The former GPT 5.6 Luna secondary tier is replaced by DeepSeek V4.1 Flash, keeping `strato` as the canonical theme name with `deepseek` as its model alias and dropping the GPT-specific `luna`/`gpt` names. The secondary tier moved from 9Router's CodeBuddy CN connection to the Command Code Provider API because `COMMANDCODE_API_KEY` provides a direct, plan-metered route to the same model. Tier model IDs remain pinned so a changing catalog cannot silently change worker behavior.

### Current setup: 2026-09-13

The `default` tier follows OpenCode through the 9Router OpenAI-compatible route:
catalog `<root>/v1/models`, adapter base `<root>/v1`, and inference
`/v1/chat/completions`. The adapter sends `reasoning_effort` and replays
`reasoning_content`. The startup fallback is replaced by exact active membership
after a successful catalog refresh. The legacy
`anthropic-compatible-providers.ts` loader name remains only to avoid a
duplicate pi auto-load migration. OpenCode already uses this route and is not
managed by tt.

The `strato` tier uses the Command Code Provider API through
`pi-worker/extensions/commandcode.ts`: base
`https://api.commandcode.ai/provider/v1`, catalog `/provider/v1/models`, Bearer
auth from `COMMANDCODE_API_KEY`. The provider is tool-compatible and accepts
`reasoning_effort`; the `high` level is transmitted literally.

The canonical 9Router measurement record lives in
`../../9router-integrations/docs/EFFORT_MATRIX.md`. On the 9Router route,
`ag/gemini-3.8-flash-high` is transmitted as `reasoning_effort=max` and
translated by the router to Gemini `thinkingLevel:high`, logged as `THINK:high`.
The registry's upstream suffix `(high)` is stripped before dispatch, so the
applied thinking comes from the client `reasoning_effort`, not the suffix. The
Gemini route exposes no client-side proof of effective effort beyond that router
log; `maxOutputTokens` is set by the thinking-level floor and then clamped to
64,000 by the Antigravity executor, so pi's `max_completion_tokens` does not
raise the ceiling.

Pi's model contract supports text and image input. Audio, video, and native
document inputs remain outside the worker path even when an upstream model
advertises them.

## Benchmark: 2026-09-14

Environment:

- One fresh worker per tier (`default` Gemini 3.8 Flash High at max effort,
  `strato` DeepSeek V4.1 Flash at high effort), identical prompt, identical
  git worktrees of the same commit, run concurrently.
- Task: build the wave 1 core of a Go CLI (module scaffold, registry
  validation, placement planner, strict lock v4, materialiser, six verbs,
  unit tests, twenty named end-to-end cases, and a byte-parity test against a
  Node reference implementation over two real presets) from a 130-line spec
  on disk. About 4.5k lines of Go per tree.
- Acceptance: gofmt, vet, `go test ./...`, parity over 708 and 714 files, no
  file at 300 lines, no dependency beyond cobra and yaml.v3, then an
  orchestrator cross-check of both binaries against the oracle and each other.

Results:

- **Gemini 3.8 Flash High:** done in 10 minutes, no provider drops, every gate
  green, all twenty named tests present, parity byte-identical. One semantic
  miss the gates did not catch: it recorded the hash of a file before its own
  structured write to it, so a fresh tree reported one customised file.
  Reported `notes: none`.
- **DeepSeek V4.1 Flash:** done in 19 minutes including two
  `Upstream stream ended before terminal chunk` drops recovered with
  `tt pi resume`; every gate green, parity byte-identical, the hash recorded
  after its own write, and six resolved spec ambiguities listed in `notes`.
  Smaller tree (36 files, constructor-style commands, tests in the command
  package) with more doc comments.
- Both trees passed every stated check; the one correctness difference was
  found only by the orchestrator's cross-check, so gate-green from either
  tier is evidence and not acceptance.
- Second round, four disjoint scopes on the winning tree, two per tier, each
  in its own worktree: DeepSeek delivered the update/merge family and the
  doctor port in 8 minutes each with no drops; its doctor matched the Node
  reference finding for finding (188 of 188), and its update family put the
  package-manager run before the lock write although the spec listed
  write, record, wire in that order, which surfaced only when a real
  `pnpm install` failed. Gemini delivered the release wiring in 14 minutes
  and the harvest port in 12, both green; the release worker spent most of
  its turn in the session directory instead of the absolute worktree path in
  its prompt and moved its files at the end, and reported `notes: none`;
  the harvest worker listed every JSON key it verified.
- Routing derived from these runs: DeepSeek for scopes where a silent semantic
  misread costs more than wall-clock time (state, provenance, merge, and
  port-fidelity work); Gemini for mechanical, wiring, configuration, and
  broad-sweep work and when speed matters. Neither tier reads an explicit
  ordering or invariant reliably enough to skip the orchestrator's own
  end-to-end run against real data. A DeepSeek task is watched through the
  pane state, because the daemon reports `interrupted` while the REPL is
  still working. A worker respawned by `tt pi clear` starts in the session
  directory, so a prompt that targets another worktree states the `cd` as
  the first step.
- Third round, the same day, twenty dispatches for a universal-tier batch
  (items, contracts, doctor rules, a 177-sentence prose sweep, compositions,
  self-adoption, a fresh-consumer proof): the Gemini route never answered
  its first request in three attempts (`Request timed out`, context stuck at
  0.2%), so every dispatch ran on DeepSeek, including the sweep and wiring
  work the routing rule had assigned to Gemini; DeepSeek handled all of it,
  with two mid-turn drops (`Stream ended without finish_reason`, a 502) that
  the REPL retried on its own while the daemon showed `interrupted`, and one
  drop that left the REPL idle without a result, recovered with `tt pi
  steer` naming the task nonce. Two workers reported `BLOCKED` with a
  precise cause instead of working around a contradiction in their contract
  (a cwd-dependent test, a guard refusing git's hook arguments, history-only
  scanner findings); both were right and the contract was the defect.
  Operational traps recorded: a worker whose editor opened a file in the
  orchestrator's main checkout left an auto-fixed, syntactically broken
  version of it there twice, so a file the orchestrator relies on is
  checked with `git status` before every gate; a preset proof that keeps its
  scratch tree exhausted /tmp's inodes after a day of runs.

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

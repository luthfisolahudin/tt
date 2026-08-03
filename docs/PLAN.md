# tt — upgrade plan: single daemon + declarative pipelines

Status: **approved, in implementation.** Working plan for the rethink round.
Read STATUS.md (current live behavior) and DESIGN.md (rationale) first; this
doc owns only the upgrade. STATUS.md "Possible next steps" defers to this plan.

## The real target (rethink conclusion)

`tt`'s foundational moat is **provider-heterogeneous, visible, durable workers** —
a pool of any-provider REPLs on their own quota, steerable in a shared tmux
substrate, with file-durable results. Claude Code's dynamic workflows /
subagents structurally cannot follow there (they are same-budget ephemeral
Claude subagents). The pool stays the product.

The pain to kill is **not** any single broken command — it is the
**coordination tax** the orchestrator pays on every delegation, in three forms:

1. **Context tax** — every `send`+`wait` round-trip and every full result body
   lands in the orchestrator's context. DESIGN already flags this as v1's core
   problem; the pool fixed the mechanics, not the context cost.
2. **Prompt tax** — half the `delegating-to-pi` skill is "make the prompt
   bulletproof because a follow-up costs a full round-trip."
3. **Visibility tax** — neither human nor agent can cleanly see what is running
   or read a window/worker without ANSI scraping (`tt x observe` exists only
   because the classifier is opaque).

Underneath all three: 2600 lines of bash with no test harness, so every fix is
expensive. The Go rewrite is the leverage point — the upgrade lands there.

## The move

`tt` becomes a **single session daemon** (`ttd`, one process for all sessions)
that owns the tmux substrate, the worker protocol, and a **declarative pipeline
engine**. Both the CLI and the agents become thin clients over a unix socket.
Work and results live in the daemon; the orchestrator triggers once and reads
one digest.

### Why one daemon, not one per session

- One process (~10–30 MB RSS) regardless of session count — the resource
  concern that motivated the question.
- Naturally owns what is already cross-session: `tt x send`, the `x observe`
  loop, the notify drainer — today three bolt-ons, absorbed into one process.
- Keeps the daemonless-work-queue spirit: **on-disk state stays the source of
  truth**; the daemon is the single writer/watcher, restartable and idempotent.
- Honest cost: a single daemon is a single point of failure across sessions.
  Mitigation is the existing one — dead daemon degrades to "CLI can't reach it,"
  never "lost work"; restart resumes from disk.

### Why declarative pipelines, not a script engine

Claude Code's dynamic workflows run untrusted JS in a sandboxed runtime — and
their docs are full of the guardrails that forces (16-agent caps, 1.5M-token
warnings, resume-ordering rules). We take the 90% of value at 10% of cost: a
pipeline is a **fixed shape defined as data**, executed by the daemon:

```
stage: fan-out   (N workers, disjoint scope)
  -> stage: review gate  (1 worker verifies each result against SUCCESS)
       pass -> stage: join (digest to orchestrator)
       fail -> re-dispatch the fan-out stage (bounded retries)
```

The two quality patterns the CC docs name — adversarial review before
reporting, and a checker that loops until green — both become a **review gate**
stage. No sandbox, no Turing-complete user code. If loops-in-code are ever
wanted, that is a separate deliberate decision.

## The four features → taxes

- **Digest-collect** *(context)* — `collect --digest` returns statuses +
  one-line notes; full bodies stay id-addressable in the daemon, pulled on
  demand. Orchestrator stops absorbing full result prose by default.
- **Pipelines with a review gate** *(context + review-step ask)* — declarative
  spec, daemon-executed, one trigger in, one digest out.
- **Cheap follow-up** *(prompt)* — daemon makes dispatch+reply fast, so a
  corrective `steer` is economical; bulletproof-prompt stops being the only path.
- **`tt peek <window|worker>`** *(visibility)* — the daemon already knows every
  pane and worker state; peek is a state query, not ANSI scraping. This is the
  "tell the AI to see window X" primitive.
- **Hierarchical, AI-friendly help** *(discoverability)* — today the entire
  reference is one 294-line heredoc printed only by top-level `tt --help`;
  every subcommand is unreachable for help and each verb `die`s on `--help`
  (`tt pi --help` → `unknown pi subcommand '--help'`, exit 1). The upgrade
  makes help **per-verb and machine-readable** (see "Help & discovery" below).

## The boundary that makes it safe

The worker control channel is **file-based and symmetric**: today bash writes
`<cs>.queue/<turn>.task` (+ `.steer` / `.resume`) and the `tt-worker` extension
polls and claims on a 200 ms loop. The daemon can therefore own the **writer**
side (task files, steer, resume triggers) and the **watcher** side (result
files, busy markers) **without changing the extension** — the extension's poll
stays the reader; the daemon is just a faster single writer. Phase 1 parity
needs no extension change. (Later, optionally, the extension can be taught to
listen on the socket instead of polling — an optimization, not a requirement.)

## Phasing — thin, reviewable, each lands green

Each phase ends with the manual throwaway-`/tmp/tt-test-*` procedure green plus
`bash -n tt` while the bash still exists. Live pi steps spend Codex quota, so
test tasks stay trivial.

### Phase 1 — `ttd` skeleton + parity spine  ✅ DONE (live-verified 2026-08-03)
- One daemon process (`ttd`), all sessions, state rooted at
  `${XDG_STATE_HOME:-$HOME/.local/state}/tt/` — socket `ttd.sock`, pidfile
  `ttd.pid` (single-instance, stale-aware). Auto-starts on first CLI call;
  `tt daemon start|stop|status|serve`.
- Daemon owns the worker-dispatch WRITE side (task files, steer/resume
  triggers, `tasks.jsonl`, spawn) and the result/notify WATCH side. Holds no
  in-memory authoritative state (disk is truth); single-writer mutex on
  file-mutating ops. Wire: one line-delimited JSON request per connection
  (`{op,session,cwd,sync_env,args}` → `{ok,stdout,stderr,exit_code,error}`);
  ops return pre-formatted stdout/stderr so the CLI relays byte-for-byte.
- Go CLI (cobra) verbs reach parity: `send`/`wait`/`status`/`collect`/
  `results`/`steer`/`resume`/`clear`/`auto`/`rm`/`remove`/`popidle`/`logs`/
  `update`. Ports of bash: lazy spawn `ensure_repl_ready`/`start_repl`
  (exact launch env), `worker_state` detection, auto policy
  (idle→spawn→pool, `--rm` ephemeral, `--prefer-fresh`), cap
  `min(cores-2,26)`, tier guards, nonce/`tasks.jsonl`/queue formats.
- **No extension change.** `pi-worker/extensions/tt-worker.ts` untouched.
- First test harness: `internal/worker/dispatch_test.go` (`go test ./...`).
- Gate PASSED live: fresh throwaway session, `send`→`wait` returned the
  nonce-validated `WORKER_DONE` envelope; `status`/`results`/`collect`/`auto
  --json` envelopes match the documented schema (`duration_s`, `elapsed_s`,
  `started_at`/`ended_at`). `build`+`vet` clean.
- Bonus (pulled from Phase 3.5): every `tt pi <verb> --help` now exits 0 with a
  scoped synopsis (cobra `Short` + a `helpRequested` intercept for the
  hand-parsed flags). The `tt pi --help` listing shows all verb summaries.
- NOT done (later phases): digest-collect, `tt peek`, pipelines, `tt x`
  send/observe (still bash), `up`/`attach`/`down` (still bash), bash
  retirement.

### Phase 2 — digest-collect + `tt peek`  ✅ DONE (live-verified 2026-08-03)
- `collect --digest` + the digest row (`<id> <status> <dur> <one-line summary>`);
  full bodies stay id-addressable via `tt pi results <id>`. `--json` keeps the
  full envelope. Gate passed: a 2-worker fan-out joined as two one-line rows;
  full body still readable on demand; collect cursor still advances
  (re-collect = "nothing new").
- `tt peek [--lines N] <window|callsign>` as a daemon state query (`peek` op,
  read-only, outside the write mutex). Accepts a bare window (`dev`), a worker
  callsign (`alfa` → `pi-alfa`), or a full `pi-<cs>`; errors cleanly on an
  unknown target. Gate passed: reads the dev shell + a worker REPL without the
  caller scraping tmux — the agent-readable "see window X" primitive.
- Build+vet+test clean. Files: `internal/daemon/watch.go` (digestLine, peekOp,
  CollectArgs.Digest), `internal/daemon/ops.go` (peek route), `cmd/peek.go`,
  `cmd/pi.go` (--digest flag).

### Phase 3 — pipeline engine  ✅ DONE (live-verified 2026-08-03)
- Declarative spec (JSON, data — no scripting): `pipeline run (FILE|-)` runs an
  ordered list of stages in the daemon, one trigger in → one digest out. Spec:
  `{name, retries, stages:[{fanout:[{label,task}...], join:"digest"|"full"} |
  {review:{prompt}}]}`. Validated up front (no stages / empty fanout / both
  kinds set / empty task / no fanout at all).
- A `fanout` stage dispatches N tasks via the auto policy (idle→spawn→pool) and
  joins each to a terminal status. A `review` stage hands the previous stage's
  results to ONE worker that ends with `PIPELINE_PASS` / `PIPELINE_FAIL:
  <reason>`; on FAIL the pipeline re-runs the preceding fanout stage, bounded
  by `retries` (default 0; exhausted → die with the reason).
- Review gate = a worker verifying each result against its SUCCESS — the two
  CC-workflow quality patterns (adversarial review, check-until-green) as a
  fixed stage, no sandbox. Dispatch critical section holds the daemon write
  mutex (turn assignment/spawn) without blocking other ops for the pipeline's
  lifetime.
- Gate PASSED live: fan-out 2 → review PASS → digest; review FAIL → bounded
  retry → PASS; FAIL with retries:0 → exit 1 with reason; spec validation
  errors. Build+vet+test clean. Files: `internal/daemon/pipeline.go`,
  `internal/daemon/ops.go` (route), `cmd/pipeline.go`.

## Help & discovery

Help is the discovery surface for a tool whose primary user is an AI agent
deciding which verb to reach for. Today it fails that job in three ways:

- **Unreachable below the top.** All help is one heredoc behind `tt --help`;
  every subcommand path (`tt pi`, `tt pi wait`, `tt x send`, …) `die`s on
  `--help`. An agent must already know the verb to learn it — discovery is
  impossible.
- **Not machine-readable.** The `--json` envelopes exist for *results*, but
  there is no structured description of the *command surface* an agent can
  enumerate (`--help --json`, a `tt verbs` listing, per-verb schemas).
- **Wall of prose.** 294 lines with no per-verb granularity means an agent
  reads the whole thing or nothing.

### The shape (lands with the Go CLI; cobra gives most of it free)

- **Per-verb help everywhere.** `tt pi --help`, `tt pi wait --help`,
  `tt x send --help` all print that verb's synopsis, flags, and one example —
  never an error. Cobra's command tree generates this from the verb
  definitions, so help can no longer drift from the parser the way the bash
  heredoc already has (the heredoc and the inline `die` parsers are two
  sources of truth).
- **One synopsis style** (from AGENTS.md): flags before positionals, source
  last — `tt <verb> [FLAGS] <positionals> (FILE|-)`; short alias first. The Go
  command definitions enforce this uniformly instead of relying on prose.
- **Machine-readable surface.** `--help --json` (or `tt verbs --json`) emits
  the verb tree with per-verb `{name, synopsis, flags[], args[], example}` so
  an agent enumerates capabilities instead of scraping prose. This pairs with
  the digest/`--json` result envelopes: the whole interface becomes
  agent-consumable.
- **Help text is generated, not handwritten.** The per-verb source of truth is
  the cobra command definition; README table, `--help`, and the consumer
  skill's `references/tt-cli.md` derive from it (extract-over-duplicate), so
  the three can no longer disagree.

This is a behavior fix, not just docs: a wrong `--help` returning exit 1 is a
bug in the discovery path.

### Phase 3.5 — help & discovery (small, independent; slots beside Phase 2/3)
- Every verb + subcommand path answers `--help` (exit 0) with a scoped
  synopsis + one example.
- `--help --json` / `tt verbs --json` emits the machine-readable verb tree.
- Help derives from the cobra definitions; README + `tt-cli.md` regenerated.
- Gate: `tt pi --help`, `tt pi wait --help`, `tt x send --help` each print
  scoped help and exit 0; `tt verbs --json` round-trips every registered verb.

### Phase 4 — retire bash  (in progress)
- Session lifecycle ported: `up` / `attach` (`a`) / `down` in Go
  (`internal/session/up.go`, `cmd/up.go`) — windows.json jq normalization
  (byte-faithful NORMALIZE_JQ), bare-shell-guarded pane commands, cold-start
  prefill semantics, window-granularity heal, dedup, version/project stamping.
- Cross-session `x` ported into the daemon: `x send` / `x list`/`ls` /
  `x observe` (`internal/daemon/x.go`, `cmd/x.go`).
- **Window-theft fix:** `tt up` inside tmux no longer `switch-client`s by
  default (an unsolicited switch replaced the caller's window — and a stray
  second session made the client toggle to the most-recent *other* session).
  New policy: in-tmux `up` builds/heals and stays put (a stderr note says how
  to enter); `--attach` switches deliberately; outside tmux `up` attaches as
  before. Verified live: in-tmux `up` against a different session keeps the
  caller on their session. This is a deliberate behavior CHANGE vs bash (which
  always switched) — the one intentional divergence in the port.
- **Full verb parity reached.** Every bash verb has a Go equivalent (audit
  found and fixed two gaps: `pi notify-drain`, which the extension spawns on
  `--notify`, and `pi steer-all`). Plus the new `peek` / `pipeline` / `daemon`.
- **Side-by-side install (`Makefile`).** `make install` puts the Go binary at
  `~/.local/bin/tt-go` for dogfooding while bash `tt` stays live and untouched;
  `make cutover` flips `~/.local/bin/tt` to the Go binary; `make restore-bash`
  reverts. Go needs a build step, so the bash-era "edits take effect
  immediately, no install step" property ends at cutover.
- Remaining before cutover: dogfood `tt-go` on real work, then flip and delete
  the bash body; docs pass (README table, DESIGN, STATUS, AGENTS.md "single-file
  bash tool", consumer skill) + MINOR version bump.
- **Unverified legs (code-reviewed only, need a live Claude Code TUI or are
  destructive):** `x send` delivery and `notify-drain` delivery (the safe-input
  classifier only accepts a real Claude TUI — a `cat` stand-in never
  classifies safe, as observed); `x observe` sqlite sampler; `down`. The
  drainer's lock/discard/stale-takeover paths ARE verified.

## Explicitly out of scope (this round)

- A Turing-complete workflow scripting engine / sandbox.
- Multi-orchestrator / agent-mesh messaging (future-proofing; today is one
  orchestrator + workers).
- Auto-starting the dev server; per-project `tt` config (still deferred from
  DESIGN's out-of-scope).

## Open implementation questions (decide at Phase 1, not blocking the plan)

- Wire format on the socket (line-based like the control files vs a tiny JSON
  RPC) — pick the simplest that round-trips the existing envelope.
- Whether `ttd` auto-starts on first `tt up` / first socket connect, or is
  `tt daemon start` explicit.
- Pipeline spec file format (likely a small JSON/YAML under `.tt/`).

## Decisions locked in the rethink

- Ambition: sharpen the worker pool (not a general agent-comms layer).
- Pipeline power: declarative spec, not an embeddable script engine.
- Bash: freeze now, keep alive until Go reaches parity, then a full switch.
- Topology: one daemon total, all sessions (resource-driven).
- Help is a discovery surface for an AI agent user: per-verb, machine-readable,
  generated from the cobra definitions (never a hand-maintained heredoc). A
  subcommand `--help` returning exit 1 is a bug.

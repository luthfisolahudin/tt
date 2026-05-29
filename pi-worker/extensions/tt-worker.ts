/**
 * tt-worker — pi extension giving `tt` a file-based control channel into a
 * live interactive pi REPL.
 *
 * Auto-loaded by tt workers from tt's private pi-worker runtime dir
 * (`PI_CODING_AGENT_DIR=~/.local/share/tt/pi-worker`). It is INERT unless
 * `TT_WORKER_CS` is set, so normal pi sessions are unaffected; `tt` sets
 * that env var only for the workers it spawns.
 *
 * Files live under `<TT_WORKER_STATE>/`, all in a dead-simple line format
 * so the bash side needs no JSON parser:
 *
 *   <cs>.queue/    a per-worker task queue (directory). Each `<turn>.task`
 *                  file is line 1 = `<task id> <tier> <nonce>` (tier+nonce
 *                  optional), rest = prompt text. tt always appends; the
 *                  extension claims the lowest-numbered task when the REPL is
 *                  idle by renaming it to `<file>.claiming`, then reads and
 *                  deletes that private path, applies the tier via
 *                  setThinkingLevel, and sends the text as a fresh user turn.
 *                  agent_end validates nonce + terminal-position. A busy
 *                  worker leaves later tasks queued until its turn ends
 *                  (send = run next; see <cs>.steer for run-now).
 *   <cs>.steer     immediate injection, bypassing the queue and tt's task
 *                  tracking: the extension consumes it (rename) and sends the
 *                  text steered into the current turn, or as a fresh untracked
 *                  turn if idle. This is `tt pi steer`.
 *   <cs>.result    lifecycle file written atomically:
 *                      id: <task id | -->
 *                      status: running|done|blocked|other|error
 *                      ---
 *                      <text>
 *                  `running` is written when a task is claimed; done/blocked/
 *                  other on `agent_end`; error for caught extension
 *                  exceptions. id is `-` for a human-typed / steered turn.
 *   <cs>.ready     written once the queue pump + steer watch are live, so tt
 *                  knows it is safe to enqueue without a startup race.
 *   <cs>.log       append-only timestamped diagnostics for failures that have
 *                  no result to attach to.
 *
 * Env: TT_WORKER_CS (callsign), TT_WORKER_STATE (tt state dir).
 */
import * as fs from "node:fs";
import * as path from "node:path";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

export default function (pi: ExtensionAPI) {
	const cs = process.env.TT_WORKER_CS;
	if (!cs) return; // inert outside tt-spawned workers

	const stateDir = process.env.TT_WORKER_STATE ?? "/tmp/tt";
	const queueDir = path.join(stateDir, `${cs}.queue`); // this worker's own queue
	const poolDir = path.join(stateDir, "queue"); // shared pool — any idle worker steals
	const poolResultsDir = path.join(stateDir, "queue-results"); // pool-<seq>.result
	const steerFile = path.join(stateDir, `${cs}.steer`);
	const resultFile = path.join(stateDir, `${cs}.result`);
	const readyFile = path.join(stateDir, `${cs}.ready`);
	const busyFile = path.join(stateDir, `${cs}.busy`);
	const logFile = path.join(stateDir, `${cs}.log`);
	let pendingId = "-";
	let pendingNonce = "";
	let busy = false; // a turn is in flight (tt-claimed task or steer-started)
	let agentCtx: any = null; // captured at session_start; exposes isIdle()

	// `busy` mirrored to a marker file so the bash side can detect "this REPL
	// is processing something" (tracked task, stolen pool task, or steer)
	// without parsing results — which a pool task writes elsewhere anyway.
	function setBusy(b: boolean) {
		busy = b;
		try {
			if (b) fs.writeFileSync(busyFile, "1");
			else fs.unlinkSync(busyFile);
		} catch {}
	}

	// Pool tasks (id `pool-<seq>`) record to a shared, id-keyed result file so a
	// steal never clobbers the stealing worker's own `.result`, and a waiter
	// polls one known path. Worker-assigned tasks use `<cs>.result`.
	function resultFileFor(id: string): string {
		return id.startsWith("pool-")
			? path.join(poolResultsDir, `${id}.result`)
			: resultFile;
	}

	function lastAssistantText(messages: any[]): string {
		let text = "";
		for (const m of messages ?? []) {
			if (m?.role !== "assistant") continue;
			const t = (m.content ?? [])
				.filter((c: any) => c?.type === "text")
				.map((c: any) => c.text)
				.join("\n");
			if (t.trim()) text = t;
		}
		return text;
	}

	function atomicWrite(file: string, data: string) {
		try {
			fs.writeFileSync(`${file}.tmp`, data);
			fs.renameSync(`${file}.tmp`, file);
		} catch {}
	}

	function logLine(msg: string) {
		try {
			fs.appendFileSync(logFile, new Date().toISOString() + " " + msg + "\n");
		} catch {}
	}

	// Claim and run the next queued task — only when idle. Synchronous up to
	// sendUserMessage, so the interval poll and agent_end can never interleave
	// mid-claim. Claiming is an atomic rename, so it is safe even when several
	// workers later share a queue (the pool queue).
	// Atomically claim the lowest-numbered `<n>.task` in `dir` and return its
	// raw contents (or null if none / lost the race). The rename is the
	// concurrency primitive: for the shared pool, many workers race here and
	// only one rename of a given file succeeds — the rest get ENOENT.
	function claimFrom(dir: string): string | null {
		let entries: string[];
		try {
			entries = fs.readdirSync(dir);
		} catch {
			return null; // dir absent (e.g. no pool yet)
		}
		const tasks = entries
			.filter((f) => f.endsWith(".task"))
			.sort((a, b) => parseInt(a, 10) - parseInt(b, 10));
		if (tasks.length === 0) return null;
		const src = path.join(dir, tasks[0]);
		const claimed = `${src}.claiming.${cs}`;
		try {
			fs.renameSync(src, claimed);
		} catch {
			return null; // another worker claimed it first
		}
		try {
			return fs.readFileSync(claimed, "utf-8");
		} catch (e) {
			logLine("read task: " + String(e));
			return null;
		} finally {
			try {
				fs.unlinkSync(claimed);
			} catch {}
		}
	}

	function pump() {
		if (busy) return;
		// Only claim when the runtime is genuinely idle. `busy` is set
		// synchronously below to close the gap before the runtime flips
		// isIdle() to false; isIdle() guards the inverse gap — agent_end fires
		// while the agent is still "processing", so claiming from there throws
		// "Agent is already processing". Claiming only from this idle-gated
		// poll keeps sendUserMessage from ever being rejected (which would
		// consume a task that then never runs).
		if (!agentCtx || !agentCtx.isIdle()) return;
		// Drain priority: this worker's own pinned queue first, then steal from
		// the shared pool. Own-queue tasks need this worker's context; pool
		// tasks are stealable for throughput.
		let raw = claimFrom(queueDir);
		if (raw === null) raw = claimFrom(poolDir);
		if (raw === null || !raw.trim()) return;
		const nl = raw.indexOf("\n");
		if (nl < 0) return; // need an id line + body
		// line 1 = `<id> <tier> <nonce>`; tier and nonce are optional.
		const head = raw.slice(0, nl).trim().split(/\s+/);
		const id = head[0] || "-";
		const tier = head[1];
		const nonce = head[2] || "";
		const text = raw.slice(nl + 1).trim();
		if (!text) return;
		pendingId = id;
		pendingNonce = nonce;
		setBusy(true);
		atomicWrite(resultFileFor(id), "id: " + id + "\nstatus: running\n---\n");
		// Reasoning effort is a runtime knob — no REPL respawn.
		if (tier === "low" || tier === "medium") {
			try {
				pi.setThinkingLevel(tier);
			} catch {}
		}
		pi.sendUserMessage(text);
	}

	pi.on("session_start", async (_event, ctx) => {
		agentCtx = ctx;
		try {
			fs.mkdirSync(queueDir, { recursive: true });
			fs.mkdirSync(poolResultsDir, { recursive: true });
			// fresh REPL → not processing anything yet
			try {
				fs.unlinkSync(busyFile);
			} catch {}
			// create-if-missing only — never clobber a steer tt may have written
			if (!fs.existsSync(steerFile)) fs.writeFileSync(steerFile, "");
		} catch (e) {
			logLine("session_start setup: " + String(e));
		}

		// Steer channel: run-now injection, separate from the queue. It does
		// not touch pendingId/pendingNonce — the in-flight task still validates
		// its own completion. Consume by rename so a concurrent tt write is
		// never clobbered.
		try {
			fs.watchFile(steerFile, { interval: 200 }, () => {
				try {
					const consuming = steerFile + ".consuming";
					try {
						fs.renameSync(steerFile, consuming);
					} catch {
						return;
					}
					let raw = "";
					try {
						raw = fs.readFileSync(consuming, "utf-8");
					} catch (e) {
						logLine("read steer: " + String(e));
						return;
					} finally {
						try {
							fs.unlinkSync(consuming);
						} catch {}
					}
					const text = raw.trim();
					if (!text) return;
					if (agentCtx && agentCtx.isIdle()) {
						// No turn to steer into — start a fresh untracked turn.
						// busy=true so the queue pump does not race a claim into it.
						setBusy(true);
						pi.sendUserMessage(text);
					} else {
						pi.sendUserMessage(text, { deliverAs: "steer" });
					}
				} catch (e) {
					logLine("steer callback: " + String(e));
				}
			});
		} catch (e) {
			logLine("steer watch setup: " + String(e));
		}

		// Queue pump: poll the worker's own queue dir and claim the next task
		// whenever the REPL is idle. A 200ms interval matches the watchFile
		// cadence and is robust where fs.watch on a directory is not.
		try {
			setInterval(() => {
				try {
					pump();
				} catch (e) {
					logLine("pump: " + String(e));
				}
			}, 200);
		} catch (e) {
			logLine("pump setup: " + String(e));
		}

		atomicWrite(readyFile, `${Date.now()}\n`);
		pump(); // pick up anything enqueued during the startup window
		if (ctx.hasUI) ctx.ui.notify(`tt-worker ${cs}: watching queue`, "info");
	});

	pi.on("agent_end", async (event: any, _ctx) => {
		// Untracked turns (steer / human-typed) have no pending task id. They
		// must NOT clobber the last tracked task's result — `tt pi wait` reads
		// that file for the tracked task. Just go idle and let the pump resume.
		if (pendingId === "-") {
			setBusy(false);
			return;
		}
		const resultDst = resultFileFor(pendingId);
		try {
			const text = lastAssistantText(event?.messages);
			// For both done and blocked: require matching nonce as a dedicated field
			// (approach 2) and verify terminal position (approach 3).
			//   - nonce: field must appear in the terminal block — prevents stale
			//     markers from prior context causing false positives on manual Esc
			//   - terminal: the block must be the last thing in the response;
			//     fenced or mid-response occurrences are ignored
			const nonceField = `nonce: ${pendingNonce}`;
			let status = "other";
			if (pendingNonce) {
				// BLOCKED block: `BLOCKED\nnonce: <N>\nreason: <...>` at end of response
				const blockedPos = text.lastIndexOf("\nBLOCKED\n");
				const blockedStart =
					blockedPos >= 0 ? blockedPos + 1 : text.startsWith("BLOCKED\n") ? 0 : -1;
				if (blockedStart >= 0) {
					const block = text.slice(blockedStart).trimEnd();
					// Terminal: only `field: value` lines after BLOCKED
					const isTerminal = /^BLOCKED(\n[\w][\w_-]*:[^\n]*)*$/.test(block);
					if (isTerminal && block.includes(nonceField)) {
						status = "blocked";
					}
				}
				// WORKER_DONE block: `WORKER_DONE\nfield: value\n...nonce: <N>` at end
				if (status === "other") {
					const wdPos = text.lastIndexOf("\nWORKER_DONE\n");
					const wdStart =
						wdPos >= 0 ? wdPos + 1 : text.startsWith("WORKER_DONE\n") ? 0 : -1;
					if (wdStart >= 0) {
						const block = text.slice(wdStart).trimEnd();
						// Terminal: only `field: value` lines after WORKER_DONE
						const isTerminal =
							/^WORKER_DONE(\n[\w][\w_-]*:[^\n]*)*$/.test(block);
						if (isTerminal && block.includes(nonceField)) status = "done";
					}
				}
			}
			atomicWrite(resultDst, `id: ${pendingId}\nstatus: ${status}\n---\n${text}\n`);
		} catch (e) {
			logLine("agent_end: " + String(e));
			atomicWrite(resultDst, "id: " + pendingId + "\nstatus: error\n---\n" + String(e) + "\n");
		} finally {
			pendingId = "-";
			pendingNonce = "";
			setBusy(false);
			// Do NOT claim the next task here: agent_end fires while the agent
			// is still "processing", so sendUserMessage would be rejected. The
			// idle-gated interval pump claims it on the next tick instead.
		}
	});
}

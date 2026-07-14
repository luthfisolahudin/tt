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
 *                  file is line 1 = `<task id> <tier> <nonce> [notify]`
 *                  (`notify` is optional and pings the orchestrator
 *                  via `tt x send` on completion), rest = prompt text. The
 *                  extension claims the lowest-numbered task when the REPL is
 *                  idle by renaming it to `<file>.claiming`, then reads and
 *                  deletes that private path, and sends the text as a fresh
 *                  user turn.
 *                  agent_end validates nonce + terminal-position. A busy
 *                  worker leaves later tasks queued until its turn ends
 *                  (send = run next; see <cs>.steer for run-now).
 *   <cs>.steer     immediate injection, bypassing the queue and tt's task
 *                  tracking: the extension consumes it (rename) and sends the
 *                  text steered into the current turn, or as a fresh untracked
 *                  turn if idle. This is `tt pi steer`.
 *   <cs>.resume    recovery trigger (presence = signal): re-drive the worker's
 *                  interrupted task to completion without a context wipe
 *                  (interrupted → busy → done). `tt pi resume` / `/tt-resume`.
 *   results/<id>.result
 *                  the id-keyed result store for EVERY task (named + pool),
 *                  written atomically:
 *                      id: <task id>
 *                      status: running|done|blocked|other|error
 *                      started_at: <epoch>   (ended_at: <epoch> once terminal)
 *                      ---
 *                      <text>
 *                  `running` is written when a task is claimed (with started_at);
 *                  done/blocked/other on `agent_end` (adds ended_at); error for
 *                  caught extension exceptions.
 *   <cs>.result    a worker's own assigned tasks are mirrored here as a
 *                  latest-pointer — the file `worker_state`/liveness reads.
 *                  Pool tasks are not mirrored (not this worker's). Untracked
 *                  turns (human/steer, id `-`) write no result at all.
 *   <cs>.busy      marker present while a turn is in flight (tracked task,
 *                  stolen pool task, or steer); tt's `worker_state` reads it.
 *   <cs>.ready     written once the queue pump + steer watch are live, so tt
 *                  knows it is safe to enqueue without a startup race.
 *   <cs>.log       append-only timestamped diagnostics for failures that have
 *                  no result to attach to.
 *
 * Also drained here: the shared pool queue `queue/` (id `pool-<seq>`, result to
 * `results/`), stolen by any idle worker after its own queue; and the
 * `--notify` queue `notify/`, to which a finished task appends `<id> <status>`
 * before spawning the `tt pi notify-drain` drainer.
 *
 * Before each worker turn it also strips loaded context-file sections between
 * `<!-- pi-worker:exclude-start -->` and `<!-- pi-worker:exclude-end -->`, so
 * ancestor/global AGENTS.md rules can stay orchestrator-only.
 *
 * Env: TT_WORKER_CS (callsign), TT_WORKER_STATE (tt state dir).
 */
import * as fs from "node:fs";
import * as path from "node:path";
import { spawn } from "node:child_process";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

type ContextFile = { path?: string; content?: string };

type StripResult = {
	text: string;
	stripped: number;
	unterminated: boolean;
};

type ContextStripResult = {
	systemPrompt: string;
	stripped: number;
	unterminatedPaths: string[];
	fallbackUsed: boolean;
};

const PI_WORKER_EXCLUDE_START = "<!-- pi-worker:exclude-start -->";
const PI_WORKER_EXCLUDE_END = "<!-- pi-worker:exclude-end -->";

function stripPiWorkerExcludedBlocks(input: string): StripResult {
	const startRe = /<!--\s*pi-worker:exclude-start\s*-->/g;
	const endRe = /<!--\s*pi-worker:exclude-end\s*-->/g;
	let text = "";
	let cursor = 0;
	let stripped = 0;
	let unterminated = false;

	while (cursor < input.length) {
		startRe.lastIndex = cursor;
		const start = startRe.exec(input);
		if (!start) {
			text += input.slice(cursor);
			break;
		}

		let removeStart = start.index;
		let lineStart = start.index;
		while (
			lineStart > cursor &&
			input[lineStart - 1] !== "\n" &&
			input[lineStart - 1] !== "\r"
		)
			lineStart--;
		if (/^[ \t]*$/.test(input.slice(lineStart, start.index)))
			removeStart = lineStart;
		text += input.slice(cursor, removeStart);
		endRe.lastIndex = start.index + start[0].length;
		const end = endRe.exec(input);
		stripped++;
		if (!end) {
			unterminated = true;
			break;
		}

		cursor = end.index + end[0].length;
		while (
			cursor < input.length &&
			(input[cursor] === " " || input[cursor] === "\t")
		)
			cursor++;
		if (input[cursor] === "\r" && input[cursor + 1] === "\n") cursor += 2;
		else if (input[cursor] === "\n") cursor++;
	}

	return { text, stripped, unterminated };
}

function stripPiWorkerExcludedContext(
	systemPrompt: string,
	contextFiles: ContextFile[],
): ContextStripResult {
	if (contextFiles.length === 0) {
		const result = stripPiWorkerExcludedBlocks(systemPrompt);
		return {
			systemPrompt: result.text,
			stripped: result.stripped,
			unterminatedPaths: result.unterminated ? ["<system prompt>"] : [],
			fallbackUsed: result.stripped > 0,
		};
	}

	let nextPrompt = systemPrompt;
	let stripped = 0;
	const unterminatedPaths: string[] = [];
	let fallbackUsed = false;

	for (const contextFile of contextFiles) {
		if (
			typeof contextFile?.path !== "string" ||
			typeof contextFile?.content !== "string"
		)
			continue;
		const result = stripPiWorkerExcludedBlocks(contextFile.content);
		if (result.stripped === 0) continue;

		stripped += result.stripped;
		if (result.unterminated) unterminatedPaths.push(contextFile.path);

		const originalBlock = `<project_instructions path="${contextFile.path}">\n${contextFile.content}\n</project_instructions>`;
		const replacementBlock = `<project_instructions path="${contextFile.path}">\n${result.text}\n</project_instructions>`;
		if (nextPrompt.includes(originalBlock)) {
			nextPrompt = nextPrompt.replace(originalBlock, replacementBlock);
		} else {
			const fallback = stripPiWorkerExcludedBlocks(nextPrompt);
			nextPrompt = fallback.text;
			fallbackUsed = true;
			if (fallback.unterminated) unterminatedPaths.push("<system prompt>");
		}
	}

	return {
		systemPrompt: nextPrompt,
		stripped,
		unterminatedPaths,
		fallbackUsed,
	};
}

export default function (pi: ExtensionAPI) {
	const cs = process.env.TT_WORKER_CS;
	if (!cs) return; // inert outside tt-spawned workers
	// Ephemeral workers (`tt pi auto --rm`) run only their own queue — never
	// steal shared-pool work — so they reliably go idle and get reaped.
	const ephemeral = process.env.TT_WORKER_EPHEMERAL === "1";

	const stateDir = process.env.TT_WORKER_STATE ?? "/tmp/tt";
	const queueDir = path.join(stateDir, `${cs}.queue`); // this worker's own queue
	const poolDir = path.join(stateDir, "queue"); // shared pool — any idle worker steals
	const resultsDir = path.join(stateDir, "results"); // <id>.result for EVERY task (named + pool)
	const steerFile = path.join(stateDir, `${cs}.steer`);
	const resumeFile = path.join(stateDir, `${cs}.resume`); // tt pi resume trigger
	const resultFile = path.join(stateDir, `${cs}.result`);
	const tasksFile = path.join(stateDir, `${cs}.tasks.jsonl`);
	const readyFile = path.join(stateDir, `${cs}.ready`);
	const busyFile = path.join(stateDir, `${cs}.busy`);
	const logFile = path.join(stateDir, `${cs}.log`);
	let pendingId = "-";
	let pendingNonce = "";
	let pendingNotify = false; // task carried --notify
	let pendingStartedAt = 0; // epoch seconds the current task was claimed (turn start)
	let busy = false; // a turn is in flight (tt-claimed task or steer-started)
	let agentCtx: any = null; // captured at session_start; exposes isIdle()
	const warnedContextExclude = new Set<string>(); // avoid repeating marker warnings every turn

	// Notify the orchestrator a task finished — append a message to the session
	// notify queue and (re)launch the single lazy drainer. Both steps are
	// instant + fire-and-forget: the worker never waits on delivery (which can
	// park for minutes on safe orchestrator input). The drainer is the only
	// thing that touches the claude pane; it coalesces and self-serializes.
	let notifySeq = 0;
	function fireNotify(id: string, status: string) {
		const session = path.basename(stateDir);
		try {
			const dir = path.join(stateDir, "notify");
			fs.mkdirSync(dir, { recursive: true });
			fs.writeFileSync(
				path.join(dir, `${Date.now()}-${process.pid}-${notifySeq++}.msg`),
				`${id} ${status}\n`,
			);
		} catch (e) {
			logLine("notify write: " + String(e));
			return;
		}
		// Spawn the drainer unconditionally — it is single-instance (a lock makes
		// a redundant one exit at once). Detached + own group so it outlives this
		// worker's reap.
		try {
			const child = spawn("tt", ["pi", "notify-drain", session], {
				detached: true,
				stdio: "ignore",
			});
			child.on("error", (e) => logLine("notify-drain spawn: " + String(e)));
			child.unref();
		} catch (e) {
			logLine("notify-drain: " + String(e));
		}
	}

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

	// Single id-keyed result store: EVERY task (named `<cs>-<turn>` and pool
	// `pool-<seq>`) records to `results/<id>.result`, so a waiter polls one known
	// path and `tt pi results` can re-read any past outcome by id. A worker's own
	// assigned tasks are ALSO mirrored to `<cs>.result` as a latest-pointer — the
	// only file `worker_state`/liveness reads (pool tasks are not this worker's,
	// so they are never mirrored and never clobber its state).
	function writeResult(id: string, data: string) {
		if (id === "-") return; // untracked turn — nothing to record
		atomicWrite(path.join(resultsDir, `${id}.result`), data);
		if (!id.startsWith("pool-")) atomicWrite(resultFile, data);
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
		if (raw === null && !ephemeral) raw = claimFrom(poolDir);
		if (raw === null || !raw.trim()) return;
		const nl = raw.indexOf("\n");
		if (nl < 0) return; // need an id line + body
		// line 1 = `<id> <tier> <nonce> [notify]`.
		const head = raw.slice(0, nl).trim().split(/\s+/);
		const id = head[0] || "-";
		const nonce = head[2] || "";
		const notify = head[3] === "notify";
		const text = raw.slice(nl + 1).trim();
		if (!text) return;
		pendingId = id;
		pendingNonce = nonce;
		pendingNotify = notify;
		pendingStartedAt = Math.floor(Date.now() / 1000);
		setBusy(true);
		writeResult(
			id,
			"id: " +
				id +
				"\nstatus: running\nstarted_at: " +
				pendingStartedAt +
				"\n---\n",
		);
		pi.sendUserMessage(text);
	}

	// --- in-place interrupt recovery (records/recovery R2) ---------------------
	// When a tracked turn ends without a valid WORKER_DONE/BLOCKED footer (an Esc
	// interrupt, a human typing over it), `agent_end` records `other`/`error` and
	// the worker is `interrupted`. These recover it WITHOUT a context wipe — the
	// live REPL keeps all its context. Driven by `/tt-resume` (typed in this
	// worker's pane) and the `<cs>.resume` trigger that `tt pi resume` writes.

	// The worker's latest-pointer (`<cs>.result`) → its id + status.
	function readLatest(): { id: string; status: string } | null {
		try {
			const raw = fs.readFileSync(resultFile, "utf-8");
			const id = (raw.match(/^id: (.*)$/m) ?? [])[1] ?? "";
			const status = (raw.match(/^status: (.*)$/m) ?? [])[1] ?? "";
			return id ? { id, status } : null;
		} catch {
			return null;
		}
	}

	// A task's nonce + notify flag from the dispatch log, by id (newest match).
	function lookupTask(id: string): { nonce: string; notify: boolean } {
		try {
			const lines = fs.readFileSync(tasksFile, "utf-8").trim().split("\n");
			for (let i = lines.length - 1; i >= 0; i--) {
				if (!lines[i].includes(`"id":"${id}"`)) continue;
				return {
					nonce: (lines[i].match(/"nonce":"([^"]*)"/) ?? [])[1] ?? "",
					notify: /"notify":1/.test(lines[i]),
				};
			}
		} catch {}
		return { nonce: "", notify: false };
	}

	function notifyUI(msg: string) {
		try {
			if (agentCtx?.hasUI)
				agentCtx.ui.notify(`tt-worker ${cs}: ${msg}`, "info");
		} catch {}
	}

	// Resume the interrupted task to completion: interrupted → busy → done.
	// Rehydrate the pending id/nonce/notify so the normal `agent_end` validator
	// closes the SAME task; the REPL context is untouched.
	function resumeInterruptedTask() {
		if (busy || (agentCtx && !agentCtx.isIdle())) {
			logLine("resume: ignored — worker busy");
			return;
		}
		const latest = readLatest();
		if (!latest || (latest.status !== "other" && latest.status !== "error")) {
			notifyUI("nothing to resume");
			return;
		}
		const { nonce, notify } = lookupTask(latest.id);
		pendingId = latest.id;
		pendingNonce = nonce;
		pendingNotify = notify;
		pendingStartedAt = Math.floor(Date.now() / 1000);
		setBusy(true);
		writeResult(
			latest.id,
			`id: ${latest.id}\nstatus: running\nstarted_at: ${pendingStartedAt}\n---\n`,
		);
		const tail = nonce
			? ` End your response with the WORKER_DONE block (or BLOCKED), using exactly \`nonce: ${nonce}\`.`
			: "";
		pi.sendUserMessage(
			`You were interrupted before finishing task ${latest.id}. Resume it from where you left off and complete the original TASK / SUCCESS criteria.${tail}`,
		);
	}

	// Slash command the human can type in this worker's own pi pane.
	try {
		pi.registerCommand("tt-resume", {
			description:
				"Resume this worker's interrupted task to completion (no context wipe)",
			handler: async () => {
				resumeInterruptedTask();
			},
		});
	} catch (e) {
		logLine("registerCommand: " + String(e));
	}

	pi.on("before_agent_start", async (event: any) => {
		try {
			if (typeof event?.systemPrompt !== "string") return;
			const contextFiles = Array.isArray(
				event?.systemPromptOptions?.contextFiles,
			)
				? event.systemPromptOptions.contextFiles
				: [];
			const result = stripPiWorkerExcludedContext(
				event.systemPrompt,
				contextFiles,
			);
			for (const filePath of result.unterminatedPaths) {
				const key = `unterminated:${filePath}`;
				if (warnedContextExclude.has(key)) continue;
				warnedContextExclude.add(key);
				logLine(
					`context exclude: ${PI_WORKER_EXCLUDE_START} in ${filePath} has no matching ${PI_WORKER_EXCLUDE_END}; stripped to end`,
				);
			}
			if (result.fallbackUsed && !warnedContextExclude.has("fallback")) {
				warnedContextExclude.add("fallback");
				logLine(
					"context exclude: used full-system-prompt fallback for marker stripping",
				);
			}
			if (result.systemPrompt !== event.systemPrompt)
				return { systemPrompt: result.systemPrompt };
		} catch (e) {
			logLine("context exclude: " + String(e));
		}
	});

	pi.on("session_start", async (_event, ctx) => {
		agentCtx = ctx;
		try {
			fs.mkdirSync(queueDir, { recursive: true });
			fs.mkdirSync(resultsDir, { recursive: true });
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

		// Recovery triggers: presence is the whole signal (no payload). Consume by
		// rename — like the steer channel — so a baseline (no file) never fires.
		const watchTrigger = (file: string, act: () => void) => {
			try {
				fs.watchFile(file, { interval: 200 }, () => {
					try {
						const consuming = file + ".consuming";
						try {
							fs.renameSync(file, consuming);
						} catch {
							return; // nothing written yet
						}
						try {
							fs.unlinkSync(consuming);
						} catch {}
						act();
					} catch (e) {
						logLine("trigger callback: " + String(e));
					}
				});
			} catch (e) {
				logLine("trigger watch setup: " + String(e));
			}
		};
		watchTrigger(resumeFile, resumeInterruptedTask);

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
				// Start index of a terminal marker: its last occurrence (or start of
				// text). The marker that appears *last* is the turn's true terminal
				// block — a mid-response BLOCKED must not beat a final WORKER_DONE.
				const markerStart = (m: string) => {
					const p = text.lastIndexOf(`\n${m}\n`);
					return p >= 0 ? p + 1 : text.startsWith(`${m}\n`) ? 0 : -1;
				};
				const wdStart = markerStart("WORKER_DONE");
				const blkStart = markerStart("BLOCKED");
				// Authentication rests on the per-task nonce: it is unguessable and
				// freshly minted per dispatch, so it cannot appear in prior context —
				// a matching `nonce:` field at/after the terminal marker is itself
				// proof this turn completed this task. We deliberately tolerate
				// multi-line field values and trailing prose: the worker contract
				// (APPEND_SYSTEM.md) still asks for one clean block, but a formatting
				// slip must not discard genuinely completed work.
				if (
					wdStart >= 0 &&
					wdStart >= blkStart &&
					text.slice(wdStart).includes(nonceField)
				) {
					status = "done";
				} else if (blkStart >= 0 && text.slice(blkStart).includes(nonceField)) {
					status = "blocked";
				}
			}
			const tsBlock = `started_at: ${pendingStartedAt}\nended_at: ${Math.floor(Date.now() / 1000)}\n`;
			writeResult(
				pendingId,
				`id: ${pendingId}\nstatus: ${status}\n${tsBlock}---\n${text}\n`,
			);
			if (pendingNotify) fireNotify(pendingId, status);
		} catch (e) {
			logLine("agent_end: " + String(e));
			const tsBlock = `started_at: ${pendingStartedAt}\nended_at: ${Math.floor(Date.now() / 1000)}\n`;
			writeResult(
				pendingId,
				"id: " +
					pendingId +
					"\nstatus: error\n" +
					tsBlock +
					"---\n" +
					String(e) +
					"\n",
			);
			if (pendingNotify) fireNotify(pendingId, "error");
		} finally {
			pendingId = "-";
			pendingNonce = "";
			pendingNotify = false;
			pendingStartedAt = 0;
			setBusy(false);
			// Do NOT claim the next task here: agent_end fires while the agent
			// is still "processing", so sendUserMessage would be rejected. The
			// idle-gated interval pump claims it on the next tick instead.
		}
	});
}

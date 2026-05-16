/**
 * tt-worker — pi extension giving `tt` a file-based control channel into a
 * live interactive pi REPL.
 *
 * Installed globally (`~/.pi/agent/settings.json` → `extensions`). It is
 * INERT unless `TT_WORKER_CS` is set, so normal pi sessions are unaffected;
 * `tt` sets that env var only for the workers it spawns.
 *
 * Files live under `<TT_WORKER_STATE>/`, all in a dead-simple line format
 * so the bash side needs no JSON parser:
 *
 *   <cs>.trigger   line 1 = task id, rest = prompt text. tt writes it;
 *                  the extension consumes it (truncates to empty) and
 *                  sends the text as a user message (steered if busy).
 *   <cs>.result    written on every `agent_end`:
 *                      id: <task id | -->
 *                      status: done|blocked|other
 *                      ---
 *                      <last assistant text, verbatim>
 *                  id is `-` for a human-typed turn.
 *   <cs>.ready     written once the trigger watch is live, so tt knows it
 *                  is safe to write a trigger without a startup race.
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
	const triggerFile = path.join(stateDir, `${cs}.trigger`);
	const resultFile = path.join(stateDir, `${cs}.result`);
	const readyFile = path.join(stateDir, `${cs}.ready`);
	let pendingId = "-";

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

	pi.on("session_start", async (_event, ctx) => {
		try {
			fs.mkdirSync(stateDir, { recursive: true });
			// create-if-missing only — never clobber a trigger tt may have
			// already written during the startup window
			if (!fs.existsSync(triggerFile)) fs.writeFileSync(triggerFile, "");
		} catch {}
		fs.watchFile(triggerFile, { interval: 200 }, () => {
			let raw = "";
			try {
				raw = fs.readFileSync(triggerFile, "utf-8");
			} catch {
				return;
			}
			if (!raw.trim()) return;
			try {
				fs.writeFileSync(triggerFile, "");
			} catch {}
			const nl = raw.indexOf("\n");
			if (nl < 0) return; // need an id line + body
			const id = raw.slice(0, nl).trim() || "-";
			const text = raw.slice(nl + 1).trim();
			if (!text) return;
			pendingId = id;
			if (ctx.isIdle()) pi.sendUserMessage(text);
			else pi.sendUserMessage(text, { deliverAs: "steer" });
		});
		atomicWrite(readyFile, `${Date.now()}\n`);
		if (ctx.hasUI) ctx.ui.notify(`tt-worker ${cs}: watching trigger`, "info");
	});

	pi.on("agent_end", async (event: any, _ctx) => {
		const text = lastAssistantText(event?.messages);
		// BLOCKED takes precedence: if the worker reported a block, that
		// stands even when it also appended a WORKER_DONE wrapper.
		let status = "other";
		if (/^BLOCKED:/m.test(text)) status = "blocked";
		else if (/^WORKER_DONE$/m.test(text)) status = "done";
		atomicWrite(resultFile, `id: ${pendingId}\nstatus: ${status}\n---\n${text}\n`);
		pendingId = "-"; // a following human turn must not reuse this id
	});
}

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
 *   <cs>.trigger   line 1 = `<task id> <tier> <nonce>` (tier+nonce optional),
 *                  rest = prompt text. tt writes it; the extension consumes
 *                  it (truncates to empty), applies the tier via
 *                  setThinkingLevel, and sends the text as a user message
 *                  (steered if busy). The prompt body ends with a required
 *                  footer; agent_end validates nonce + terminal-position.
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
	let pendingNonce = "";

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
			// line 1 = `<id> <tier> <nonce>`; tier and nonce are optional.
			const head = raw.slice(0, nl).trim().split(/\s+/);
			const id = head[0] || "-";
			const tier = head[1];
			const nonce = head[2] || "";
			const text = raw.slice(nl + 1).trim();
			if (!text) return;
			pendingId = id;
			pendingNonce = nonce;
			// Reasoning effort is a runtime knob — no REPL respawn.
			if (tier === "low" || tier === "medium") {
				try {
					pi.setThinkingLevel(tier);
				} catch {}
			}
			if (ctx.isIdle()) pi.sendUserMessage(text);
			else pi.sendUserMessage(text, { deliverAs: "steer" });
		});
		atomicWrite(readyFile, `${Date.now()}\n`);
		if (ctx.hasUI) ctx.ui.notify(`tt-worker ${cs}: watching trigger`, "info");
	});

	pi.on("agent_end", async (event: any, _ctx) => {
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
			// BLOCKED: `BLOCKED: <reason>\nnonce: <N>` at end of response
			const blockedPos = text.lastIndexOf("\nBLOCKED:");
			const blockedStart =
				blockedPos >= 0 ? blockedPos + 1 : text.startsWith("BLOCKED:") ? 0 : -1;
			if (blockedStart >= 0) {
				const block = text.slice(blockedStart).trimEnd();
				// Terminal BLOCKED block: BLOCKED: line + optional `nonce:` field
				const isTerminal = /^BLOCKED:[^\n]*(\nnonce:[^\n]*)?$/.test(block);
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
		atomicWrite(resultFile, `id: ${pendingId}\nstatus: ${status}\n---\n${text}\n`);
		pendingId = "-";
		pendingNonce = "";
	});
}

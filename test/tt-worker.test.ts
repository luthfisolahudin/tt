import assert from "node:assert/strict";
import test from "node:test";

import {
	stripPiWorkerExcludedBlocks,
	stripPiWorkerExcludedContext,
} from "../pi-worker/extensions/tt-worker.ts";

const START = "<!-- pi-worker:exclude-start -->";
const END = "<!-- pi-worker:exclude-end -->";

test("strips a marked block and keeps surrounding text", () => {
	const result = stripPiWorkerExcludedBlocks(`before\n${START}\nsecret\n${END}\nafter\n`);
	assert.equal(result.text, "before\nafter\n");
	assert.equal(result.stripped, 1);
	assert.equal(result.unterminated, false);
});

test("strips multiple marked blocks", () => {
	const input = `a\n${START}\none\n${END}\nb\n${START}\ntwo\n${END}\nc`;
	const result = stripPiWorkerExcludedBlocks(input);
	assert.equal(result.text, "a\nb\nc");
	assert.equal(result.stripped, 2);
	assert.equal(result.unterminated, false);
});

test("reports an unterminated block", () => {
	const result = stripPiWorkerExcludedBlocks(`keep\n${START}\nsecret`);
	assert.equal(result.text, "keep\n");
	assert.equal(result.stripped, 1);
	assert.equal(result.unterminated, true);
});

test("accepts marker whitespace", () => {
	const input = `a\n<!--  pi-worker:exclude-start  -->\nx\n<!-- pi-worker:exclude-end   -->\nb\n`;
	const result = stripPiWorkerExcludedBlocks(input);
	assert.equal(result.text, "a\nb\n");
	assert.equal(result.stripped, 1);
	assert.equal(result.unterminated, false);
});

test("strips system prompt blocks when there are no context files", () => {
	const result = stripPiWorkerExcludedContext(`head\n${START}\nsecret\n${END}\ntail\n`, []);
	assert.equal(result.systemPrompt, "head\ntail\n");
	assert.equal(result.stripped, 1);
	assert.deepEqual(result.unterminatedPaths, []);
	assert.equal(result.fallbackUsed, true);
});

test("replaces a matching project_instructions block", () => {
	const content = `keep\n${START}\nsecret\n${END}\nkeep2\n`;
	const prompt = `prefix\n<project_instructions path="AGENTS.md">\n${content}\n</project_instructions>\nsuffix\n`;
	const result = stripPiWorkerExcludedContext(prompt, [{ path: "AGENTS.md", content }]);
	assert.equal(result.stripped, 1);
	assert.equal(result.fallbackUsed, false);
	assert.deepEqual(result.unterminatedPaths, []);
	assert.ok(!result.systemPrompt.includes("secret"));
	assert.ok(result.systemPrompt.includes("keep"));
	assert.ok(result.systemPrompt.includes("keep2"));
});

test("falls back to stripping the prompt when the block is not found", () => {
	const content = `keep\n${START}\nsecret\n${END}\n`;
	const prompt = `head\n${START}\nsecret\n${END}\ntail\n`;
	const result = stripPiWorkerExcludedContext(prompt, [{ path: "missing.md", content }]);
	assert.equal(result.stripped, 1);
	assert.equal(result.fallbackUsed, true);
	assert.ok(!result.systemPrompt.includes("secret"));
});

test("records unterminated context paths", () => {
	const content = `keep\n${START}\nsecret`;
	const prompt = `<project_instructions path="AGENTS.md">\n${content}\n</project_instructions>`;
	const result = stripPiWorkerExcludedContext(prompt, [{ path: "AGENTS.md", content }]);
	assert.deepEqual(result.unterminatedPaths, ["AGENTS.md"]);
});

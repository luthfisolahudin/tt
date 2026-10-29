import assert from "node:assert/strict";
import test from "node:test";

import registerCommandCode, { toModel } from "../pi-worker/extensions/commandcode.ts";

type CapturedProvider = {
	api: string;
	baseUrl: string;
	models: ReturnType<typeof toModel>[];
	name: string;
	refreshModels: (options: {
		allowNetwork: boolean;
		signal: AbortSignal;
	}) => Promise<ReturnType<typeof toModel>[]>;
};

function register(apiKey: string | undefined): CapturedProvider | undefined {
	const previous = process.env.COMMANDCODE_API_KEY;
	if (apiKey === undefined) delete process.env.COMMANDCODE_API_KEY;
	else process.env.COMMANDCODE_API_KEY = apiKey;

	let captured: CapturedProvider | undefined;
	const fake = {
		registerProvider: (_name: string, provider: CapturedProvider) => {
			captured = provider;
		},
	};

	try {
		registerCommandCode(fake as never);
		return captured;
	} finally {
		if (previous === undefined) delete process.env.COMMANDCODE_API_KEY;
		else process.env.COMMANDCODE_API_KEY = previous;
	}
}

test("maps reasoning and vision ids", () => {
	const model = toModel({
		id: "google/gemini-3.8-flash",
		name: "Gemini",
		context_length: 1_000_000,
	});
	assert.equal(model.reasoning, true);
	assert.deepEqual(model.input, ["text", "image"]);
	assert.equal(model.contextWindow, 1_000_000);
	assert.equal(model.maxTokens, 65_536);
	assert.deepEqual(model.thinkingLevelMap, {
		off: null,
		minimal: "low",
		low: "low",
		medium: "medium",
		high: "high",
		xhigh: "high",
		max: "high",
	});
	assert.deepEqual(model.compat, {
		supportsReasoningEffort: true,
		thinkingFormat: "openai",
	});
});

test("keeps unknown models text-only and non-reasoning", () => {
	const model = toModel({ id: "unknown/model" });
	assert.equal(model.reasoning, false);
	assert.deepEqual(model.input, ["text"]);
	assert.equal(model.contextWindow, 128_000);
	assert.equal(model.maxTokens, 65_536);
	assert.equal(model.thinkingLevelMap, undefined);
	assert.equal(model.compat, undefined);
});

test("clamps output tokens to the context window", () => {
	const model = toModel({ id: "unknown/small", context_length: 1024 });
	assert.equal(model.contextWindow, 1024);
	assert.equal(model.maxTokens, 1024);
});

test("registers the provider with fallback models", () => {
	const provider = register("test-key");
	assert.ok(provider);
	assert.equal(provider.name, "Command Code");
	assert.equal(provider.baseUrl, "https://api.commandcode.ai/provider/v1");
	assert.equal(provider.api, "openai-completions");
	assert.deepEqual(
		provider.models.map((model) => model.id),
		["deepseek/deepseek-v4.1-flash", "google/gemini-3.8-flash"],
	);
});

test("does nothing without an API key", () => {
	const original = process.emitWarning;
	const warnings: string[] = [];
	process.emitWarning = ((message: string | Error) => {
		warnings.push(typeof message === "string" ? message : message.message);
	}) as typeof process.emitWarning;
	try {
		assert.equal(register(undefined), undefined);
		assert.equal(warnings.length, 1);
	} finally {
		process.emitWarning = original;
	}
});

test("serves cached fallbacks when network refresh is disabled", async () => {
	const provider = register("test-key");
	assert.ok(provider);
	const models = await provider.refreshModels({
		allowNetwork: false,
		signal: new AbortController().signal,
	});
	assert.deepEqual(
		models.map((model) => model.id),
		["deepseek/deepseek-v4.1-flash", "google/gemini-3.8-flash"],
	);
});

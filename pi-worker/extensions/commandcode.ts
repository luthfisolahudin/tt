import type { ExtensionAPI, ProviderModelConfig } from "@earendil-works/pi-coding-agent";

type ChatProviderModelConfig = Extract<ProviderModelConfig, { type?: "chat" }>;

const BASE_URL = "https://api.commandcode.ai/provider/v1";
const DEFAULT_MAX_TOKENS = 65_536;

// Command Code publishes no per-model reasoning/vision flags, so the catalog is
// projected from id patterns. Unknown ids stay text-only and non-reasoning.
const REASONING_ID = [
	/deepseek/i,
	/gemini/i,
	/glm/i,
	/kimi/i,
	/qwen/i,
	/minimax/i,
	/mimo/i,
	/grok/i,
	/gpt-5/i,
	/muse-spark/i,
	/inkling/i,
	/step-3/i,
	/nemotron/i,
	/hy[34]/i,
	/fugu/i,
	/longcat/i,
	/laguna/i,
	/ling-3/i,
];
const VISION_ID = [/gemini/i, /glm/i, /kimi/i, /muse-spark/i, /deepseek/i];

const THINKING_LEVEL_MAP = {
	off: null,
	minimal: "low",
	low: "low",
	medium: "medium",
	high: "high",
	xhigh: "high",
	max: "high",
} satisfies NonNullable<ChatProviderModelConfig["thinkingLevelMap"]>;

interface CommandCodeRecord {
	id?: unknown;
	name?: unknown;
	context_length?: unknown;
}

function matches(id: string, patterns: RegExp[]): boolean {
	return patterns.some((pattern) => pattern.test(id));
}

export function toModel(record: CommandCodeRecord): ChatProviderModelConfig {
	const id = String(record.id ?? "").trim();
	const contextWindow = Number.isFinite(record.context_length)
		? Number(record.context_length)
		: 128_000;
	const reasoning = matches(id, REASONING_ID);
	return {
		id,
		name: typeof record.name === "string" && record.name.length > 0 ? record.name : id,
		reasoning,
		input: matches(id, VISION_ID) ? ["text", "image"] : ["text"],
		cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
		contextWindow,
		maxTokens: Math.min(DEFAULT_MAX_TOKENS, contextWindow),
		...(reasoning
			? {
					thinkingLevelMap: THINKING_LEVEL_MAP,
					compat: { supportsReasoningEffort: true, thinkingFormat: "openai" },
				}
			: {}),
	};
}

const FALLBACK_MODELS: ChatProviderModelConfig[] = [
	{ id: "deepseek/deepseek-v4.1-flash", name: "DeepSeek V4.1 Flash", context_length: 1_000_000 },
	{ id: "google/gemini-3.8-flash", name: "Gemini 3.8 Flash", context_length: 1_000_000 },
].map(toModel);

async function fetchModels(
	apiKey: string,
	signal: AbortSignal,
): Promise<ChatProviderModelConfig[]> {
	const response = await fetch(`${BASE_URL}/models`, {
		headers: { Authorization: `Bearer ${apiKey}` },
		signal,
	});
	if (!response.ok)
		throw new Error(`Command Code model discovery failed (HTTP ${response.status})`);
	const payload: unknown = await response.json();
	const data = Array.isArray(payload)
		? payload
		: Array.isArray((payload as { data?: unknown })?.data)
			? (payload as { data: unknown[] }).data
			: [];
	const models = data
		.map((entry) => toModel((entry ?? {}) as CommandCodeRecord))
		.filter((model) => model.id.length > 0);
	if (models.length === 0) throw new Error("Command Code returned an empty model catalog");
	return models;
}

function warn(message: string): void {
	process.emitWarning(message, { type: "CommandCodeProviderWarning" });
}

/** Registers the Command Code Provider API (OpenAI Chat Completions). */
export default function registerCommandCode(pi: ExtensionAPI): void {
	const apiKey = process.env.COMMANDCODE_API_KEY?.trim();
	if (!apiKey) {
		warn("Command Code provider disabled: COMMANDCODE_API_KEY is not set");
		return;
	}
	let models = FALLBACK_MODELS;
	pi.registerProvider("commandcode", {
		name: "Command Code",
		baseUrl: BASE_URL,
		api: "openai-completions",
		apiKey,
		authHeader: true,
		models,
		refreshModels: async ({ allowNetwork, signal }) => {
			if (!allowNetwork || signal.aborted) return models;
			try {
				models = await fetchModels(apiKey, signal);
			} catch (error) {
				warn(
					`Command Code model discovery failed: ${error instanceof Error ? error.message : String(error)}`,
				);
			}
			return models;
		},
	});
}

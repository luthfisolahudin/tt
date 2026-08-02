import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { anthropicMessagesApi, createProvider } from "@earendil-works/pi-ai/compat";
import type { ApiKeyAuth, Model, ProviderAuth, RefreshModelsContext } from "@earendil-works/pi-ai/compat";

type AnthropicModel = Model<"anthropic-messages">;
type ModelRecord = Record<string, unknown>;

const ZERO_COST = { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 };
const LOCAL_URL = "http://127.0.0.1:20128";
const LOCAL_KEY = "sk_9router";
const KIMI_K3_ID = "cbcn/kimi-k3";
const CODEBUDDY_THINKING_LEVEL_MAP = {
	off: null,
	minimal: null,
	low: null,
	medium: null,
	high: null,
	xhigh: null,
	max: "max",
} satisfies NonNullable<AnthropicModel["thinkingLevelMap"]>;

function positiveNumber(...values: unknown[]): number | undefined {
	for (const value of values) {
		if (typeof value === "number" && Number.isFinite(value) && value > 0) return value;
	}
	return undefined;
}

function model(
	provider: string,
	baseUrl: string,
	id: string,
	name: string,
	options: Pick<AnthropicModel, "reasoning" | "input" | "contextWindow" | "maxTokens"> &
		Partial<Pick<AnthropicModel, "thinkingLevelMap" | "compat">>,
): AnthropicModel {
	return {
		id,
		name,
		api: "anthropic-messages",
		provider,
		baseUrl,
		...options,
		cost: ZERO_COST,
	};
}

function localFallback(): AnthropicModel {
	return model("9router", LOCAL_URL, "cbcn/deepseek-v4-flash", "DeepSeek V4 Flash (CodeBuddy CN)", {
		reasoning: true,
		input: ["text", "image"],
		contextWindow: 1000000,
		maxTokens: 50000,
		thinkingLevelMap: CODEBUDDY_THINKING_LEVEL_MAP,
		compat: { forceAdaptiveThinking: true },
	});
}

function kimiK3Fallback(): AnthropicModel {
	return model("9router", LOCAL_URL, KIMI_K3_ID, "Kimi K3 (CodeBuddy CN)", {
		reasoning: true,
		input: ["text", "image"],
		contextWindow: 1_048_576,
		maxTokens: 131_072,
		thinkingLevelMap: CODEBUDDY_THINKING_LEVEL_MAP,
		compat: { forceAdaptiveThinking: true },
	});
}

function records(payload: unknown): ModelRecord[] {
	if (Array.isArray(payload)) return payload.filter((entry): entry is ModelRecord => !!entry && typeof entry === "object");
	if (!payload || typeof payload !== "object") return [];
	const root = payload as ModelRecord;
	for (const value of [root.data, root.models]) {
		if (Array.isArray(value)) return value.filter((entry): entry is ModelRecord => !!entry && typeof entry === "object");
	}
	return typeof root.id === "string" ? [root] : [];
}

function discoveredModel(provider: string, baseUrl: string, entry: ModelRecord): AnthropicModel | undefined {
	const id = typeof entry.id === "string" ? entry.id : undefined;
	if (!id) return undefined;
	if (provider === "9router" && id === "cbcn/deepseek-v4-flash") return localFallback();
	if (provider === "9router" && id === KIMI_K3_ID) return kimiK3Fallback();

	const metadata = entry.metadata && typeof entry.metadata === "object" ? (entry.metadata as ModelRecord) : {};
	const capabilities = entry.capabilities && typeof entry.capabilities === "object" ? (entry.capabilities as ModelRecord) : {};
	const codeBuddyReasoning = provider === "9router" && id.startsWith("cbcn/") && capabilities.reasoning === true;
	return model(provider, baseUrl, id, typeof entry.name === "string" ? entry.name : typeof entry.display_name === "string" ? entry.display_name : id, {
		reasoning: capabilities.reasoning === true,
		input: capabilities.vision === true ? ["text", "image"] : ["text"],
		...(codeBuddyReasoning
			? { thinkingLevelMap: CODEBUDDY_THINKING_LEVEL_MAP, compat: { forceAdaptiveThinking: true } }
			: {}),
		contextWindow: positiveNumber(
			capabilities.contextWindow,
			entry.context_window,
			entry.contextWindow,
			entry.context_length,
			entry.input_token_limit,
			metadata.context_window,
			metadata.contextWindow,
			metadata.context_length,
			metadata.input_token_limit,
		) ?? 128000,
		maxTokens: positiveNumber(
			capabilities.maxOutput,
			entry.max_tokens,
			entry.maxTokens,
			entry.max_output_tokens,
			entry.output_token_limit,
			metadata.max_tokens,
			metadata.maxTokens,
			metadata.max_output_tokens,
			metadata.output_token_limit,
		) ?? 16384,
	});
}

async function fetchCatalog(
	providerId: string,
	label: string,
	baseUrl: string,
	context: RefreshModelsContext,
	fallbackKey?: string,
): Promise<readonly AnthropicModel[]> {
	const key = context.credential?.type === "api_key" ? context.credential.key : fallbackKey;
	if (!key) throw new Error(`${label} model discovery requires a configured API key`);

	let response: Response;
	try {
		response = await fetch(`${baseUrl}/v1/models`, {
			headers: { Authorization: `Bearer ${key}` },
			signal: context.signal,
		});
	} catch {
		if (context.signal?.aborted) throw new Error(`${label} model discovery was aborted`);
		throw new Error(`${label} model discovery request failed`);
	}
	if (!response.ok) throw new Error(`${label} model discovery failed (HTTP ${response.status})`);

	let payload: unknown;
	try {
		payload = await response.json();
	} catch {
		throw new Error(`${label} model discovery returned invalid JSON`);
	}

	const discovered = new Map<string, AnthropicModel>();
	for (const entry of records(payload)) {
		const item = discoveredModel(providerId, baseUrl, entry);
		if (item) discovered.set(item.id, item);
	}
	if (providerId === "9router" && !discovered.has(KIMI_K3_ID)) {
		discovered.set(KIMI_K3_ID, kimiK3Fallback());
	}
	if (!discovered.size) throw new Error(`${label} model discovery returned no model records`);
	return [...discovered.values()];
}

const localAuth: ApiKeyAuth = {
	name: "9Router key",
	async login(interaction) {
		return { type: "api_key", key: await interaction.prompt({ type: "secret", message: "9Router key" }) };
	},
	async resolve({ credential }) {
		return {
			auth: { apiKey: process.env.NINE_ROUTER_API_KEY ?? credential?.key ?? LOCAL_KEY },
			source: process.env.NINE_ROUTER_API_KEY ? "NINE_ROUTER_API_KEY" : credential?.key ? "stored API key" : "loopback key",
		};
	},
};

function providerAuth(apiKey: ApiKeyAuth): ProviderAuth {
	return { apiKey };
}

export default function (pi: ExtensionAPI) {
	pi.registerProvider(
		createProvider({
			id: "9router",
			name: "9Router",
			baseUrl: LOCAL_URL,
			auth: providerAuth(localAuth),
			models: [localFallback(), kimiK3Fallback()],
			fetchModels: (context) => fetchCatalog("9router", "9Router", LOCAL_URL, context, process.env.NINE_ROUTER_API_KEY ?? LOCAL_KEY),
			api: anthropicMessagesApi(),
		}),
	);
}

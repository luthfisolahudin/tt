import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { anthropicMessagesApi, envApiKeyAuth, createProvider } from "@earendil-works/pi-ai/compat";
import type { ApiKeyAuth, Model, ProviderAuth, RefreshModelsContext } from "@earendil-works/pi-ai/compat";

type AnthropicModel = Model<"anthropic-messages">;
type ModelRecord = Record<string, unknown>;

const ZERO_COST = { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 };
const LOCAL_URL = "http://127.0.0.1:8317";
const COSMOS_URL = "https://api.cosmoshub.tech";

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
		Partial<Pick<AnthropicModel, "thinkingLevelMap">>,
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
	return model("cliproxy", LOCAL_URL, "gemini-3.6-flash-high", "Gemini 3.6 Flash High", {
		reasoning: true,
		input: ["text", "image"],
		contextWindow: 1048576,
		maxTokens: 65536,
		thinkingLevelMap: { off: null, minimal: null, low: null, medium: null, high: "high", xhigh: null, max: null },
	});
}

function cosmosFallback(): AnthropicModel {
	return model("cosmoshub", COSMOS_URL, "qwen-3.7-max", "Qwen 3.7 Max", {
		reasoning: true,
		input: ["text"],
		contextWindow: 991000,
		maxTokens: 65536,
		thinkingLevelMap: { off: null, minimal: null, low: null, medium: null, high: null, xhigh: "xhigh", max: "max" },
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
	if (provider === "cliproxy" && id === "gemini-3.6-flash-high") return localFallback();
	if (provider === "cosmoshub" && id === "qwen-3.7-max") return cosmosFallback();

	const metadata = entry.metadata && typeof entry.metadata === "object" ? (entry.metadata as ModelRecord) : {};
	return model(provider, baseUrl, id, typeof entry.name === "string" ? entry.name : typeof entry.display_name === "string" ? entry.display_name : id, {
		reasoning: false,
		input: ["text"],
		contextWindow: positiveNumber(
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
): Promise<readonly AnthropicModel[]> {
	const key = context.credential?.type === "api_key" ? context.credential.key : undefined;
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
	if (!discovered.size) throw new Error(`${label} model discovery returned no model records`);
	return [...discovered.values()];
}

const localAuth: ApiKeyAuth = {
	name: "CLIProxyAPI key",
	async login(interaction) {
		return { type: "api_key", key: await interaction.prompt({ type: "secret", message: "CLIProxyAPI key" }) };
	},
	async resolve({ credential }) {
		return credential?.key ? { auth: { apiKey: credential.key }, source: "stored API key" } : undefined;
	},
};

function providerAuth(apiKey: ApiKeyAuth): ProviderAuth {
	return { apiKey };
}

export default function (pi: ExtensionAPI) {
	pi.registerProvider(
		createProvider({
			id: "cliproxy",
			name: "CLIProxyAPI",
			baseUrl: LOCAL_URL,
			auth: providerAuth(localAuth),
			models: [localFallback()],
			fetchModels: (context) => fetchCatalog("cliproxy", "CLIProxyAPI", LOCAL_URL, context),
			api: anthropicMessagesApi(),
		}),
	);

	pi.registerProvider(
		createProvider({
			id: "cosmoshub",
			name: "CosmosHub",
			baseUrl: COSMOS_URL,
			auth: providerAuth(envApiKeyAuth("CosmosHub API key", ["COSMOSHUB_API_KEY"])),
			models: [cosmosFallback()],
			fetchModels: (context) => fetchCatalog("cosmoshub", "CosmosHub", COSMOS_URL, context),
			api: anthropicMessagesApi(),
		}),
	);
}
